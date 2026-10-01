package launch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerservice"
)

// modelUpdateGateway is the GatewayCoordination fixture for the update path. It
// mints one managed key per call for the exact model set and records how many keys
// it issued and revoked, so a replay can be told from a second key.
type modelUpdateGateway struct {
	api.GatewayCoordinationClient
	mints, revokes int
	lastMint       *api.ManagedKeyCommand
	lastRevoke     *api.ManagedKeyRevoke
	mintErr        error
	revokeErr      error
}

func (g *modelUpdateGateway) CreateManagedKey(_ context.Context, r *api.ManagedKeyCommand, _ ...grpc.CallOption) (*api.ManagedKeyBinding, error) {
	g.mints++
	g.lastMint = proto.Clone(r).(*api.ManagedKeyCommand)
	if g.mintErr != nil {
		return nil, g.mintErr
	}
	return &api.ManagedKeyBinding{KeyBindingId: "gateway-key-" + strings.Join(r.GetModelIds(), "_"), Fingerprint: "sha256:" + strings.Repeat("cd", 32), SecretDeliveryReference: contracts.WorkspaceGatewaySecretRef(r.GetWorkspaceId()), WorkspaceId: r.GetWorkspaceId(), TargetRuntimeInstanceId: r.GetTargetRuntimeInstanceId()}, nil
}

func (g *modelUpdateGateway) RevokeManagedKey(_ context.Context, r *api.ManagedKeyRevoke, _ ...grpc.CallOption) (*api.Operation, error) {
	g.revokes++
	g.lastRevoke = proto.Clone(r).(*api.ManagedKeyRevoke)
	if g.revokeErr != nil {
		return nil, g.revokeErr
	}
	return &api.Operation{OperationId: r.GetKeyBindingId(), Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_GATEWAY, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_REVOKE_KEY, ResourceId: r.GetKeyBindingId(), Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_SUCCEEDED}, nil
}

// modelUpdateFabric is the FabricCoordination fixture. It models the merged Fabric
// contract exactly: one active binding per runtime and purpose. BindSecret confirms
// the first binding and replays that same row for an identical request, refusing a
// different Secret. A later configuration reaches RebindSecret, which requires the
// expected active predecessor, confirms the replacement through the provider, retires
// the predecessor and returns the replacement as the one active binding.
type modelUpdateFabric struct {
	api.FabricCoordinationClient
	binds        int
	rebinds      int
	lastBinding  *api.SecretBindingCommand
	lastRebind   *api.SecretBindingRebindCommand
	bindErr      error
	rebindErr    error
	rejected     bool
	rebindReject bool
	predecessor  string
	active       string
	rows         map[string]*api.SecretBindingReadback
}

func (f *modelUpdateFabric) BindSecret(_ context.Context, r *api.SecretBindingCommand, _ ...grpc.CallOption) (*api.SecretBindingReadback, error) {
	f.binds++
	f.lastBinding = proto.Clone(r).(*api.SecretBindingCommand)
	if f.bindErr != nil {
		return nil, f.bindErr
	}
	if f.rejected {
		return &api.SecretBindingReadback{SecretBindingId: "sbx_" + r.GetRuntimeInstanceId(), RuntimeInstanceId: r.GetRuntimeInstanceId(), Fingerprint: "sha256:other", Version: "other", Outcome: api.Observation_OBSERVATION_CONFIRMED}, nil
	}
	key := r.GetRuntimeInstanceId() + ":" + r.GetTargetSlot()
	if f.rows == nil {
		f.rows = map[string]*api.SecretBindingReadback{}
	}
	if stored, found := f.rows[key]; found {
		if stored.GetFingerprint() != r.GetFingerprint() {
			return nil, status.Error(codes.AlreadyExists, "runtime already has a bound Secret for this purpose")
		}
		return proto.Clone(stored).(*api.SecretBindingReadback), nil
	}
	fresh := &api.SecretBindingReadback{SecretBindingId: "sbx_" + r.GetRuntimeInstanceId() + "_" + strings.TrimPrefix(r.GetKeyBindingId(), "gateway-key-"), RuntimeInstanceId: r.GetRuntimeInstanceId(), Fingerprint: r.GetFingerprint(), Version: "v" + strings.TrimPrefix(r.GetKeyBindingId(), "gateway-key-"), Outcome: api.Observation_OBSERVATION_CONFIRMED}
	f.rows[key] = fresh
	f.active = fresh.GetSecretBindingId()
	return proto.Clone(fresh).(*api.SecretBindingReadback), nil
}

func (f *modelUpdateFabric) RebindSecret(_ context.Context, r *api.SecretBindingRebindCommand, _ ...grpc.CallOption) (*api.SecretBindingRebindReadback, error) {
	f.rebinds++
	f.lastRebind = proto.Clone(r).(*api.SecretBindingRebindCommand)
	if f.rebindErr != nil {
		return nil, f.rebindErr
	}
	if f.rebindReject {
		return &api.SecretBindingRebindReadback{PreviousSecretBindingId: r.GetExpectedCurrentSecretBindingId(), SecretBindingId: f.active, RuntimeInstanceId: r.GetRuntimeInstanceId(), Fingerprint: "sha256:other", Version: "other", Outcome: api.Observation_OBSERVATION_CONFIRMED}, nil
	}
	if f.active == "" || f.active != r.GetExpectedCurrentSecretBindingId() {
		return nil, status.Error(codes.FailedPrecondition, "the active Secret binding does not match the expected predecessor")
	}
	key := r.GetRuntimeInstanceId() + ":" + r.GetTargetSlot()
	previous := f.active
	fresh := &api.SecretBindingReadback{SecretBindingId: "sbx_" + r.GetRuntimeInstanceId() + "_" + strings.TrimPrefix(r.GetKeyBindingId(), "gateway-key-"), RuntimeInstanceId: r.GetRuntimeInstanceId(), Fingerprint: r.GetFingerprint(), Version: "v" + strings.TrimPrefix(r.GetKeyBindingId(), "gateway-key-"), Outcome: api.Observation_OBSERVATION_CONFIRMED}
	if f.rows == nil {
		f.rows = map[string]*api.SecretBindingReadback{}
	}
	f.rows[key] = fresh
	f.active = fresh.GetSecretBindingId()
	return &api.SecretBindingRebindReadback{PreviousSecretBindingId: previous, SecretBindingId: fresh.GetSecretBindingId(), RuntimeInstanceId: r.GetRuntimeInstanceId(), Fingerprint: fresh.GetFingerprint(), Version: fresh.GetVersion(), Outcome: api.Observation_OBSERVATION_CONFIRMED, ReceiptId: fresh.GetSecretBindingId()}, nil
}

// modelUpdateServe is the ServeAgentCoordination fixture. It reports the applied
// version Serve's own reload confirmed, and records the exact reload command so the
// opaque binding the Workspace presented is observable.
type modelUpdateServe struct {
	api.ServeAgentCoordinationClient
	reloads      int
	lastReload   *api.RuntimeReloadCommand
	reloadErr    error
	serveStatus  api.OperationStatusEnum
	operationNil bool
}

func (s *modelUpdateServe) ReloadModels(_ context.Context, r *api.RuntimeReloadCommand, _ ...grpc.CallOption) (*api.Operation, error) {
	s.reloads++
	s.lastReload = proto.Clone(r).(*api.RuntimeReloadCommand)
	if s.reloadErr != nil {
		return nil, s.reloadErr
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
// resolved application source and a runtime command, then wires the update path's
// three coordination stubs. It returns the service and its stubs.
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
	result, err := json.Marshal(orderResult{GrantID: "grant-original", ApplicationSource: sourceRaw, RuntimeCommand: wire(command)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(t.Context(), `UPDATE workspace.operations SET result=$2 WHERE id=$1`, op.ID, result); err != nil {
		t.Fatal(err)
	}
	gateway, fabric, serve := &modelUpdateGateway{}, &modelUpdateFabric{}, &modelUpdateServe{}
	service.Auth = ownerservice.NewAuthorizer(ownerservice.OwnerWorkspace, &modelReadIdentity{})
	service.Gateway, service.Fabric, service.Serve = gateway, fabric, serve
	return service, gateway, fabric, serve
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

// TestUpdateWorkspaceModelsAppliesOneOwnerSeparatedConfiguration proves the whole
// accepted sequence: Workspace versions the intent, Gateway issues the managed key
// for the exact model set, Fabric confirms the runtime Secret binding, Serve applies
// and reads back the configuration, and only then does the Workspace advance its
// applied version.
func TestUpdateWorkspaceModelsAppliesOneOwnerSeparatedConfiguration(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, fabric, serve := modelUpdateWorkspace(t, db, true)
	call, ctx := modelUpdateContext("tenant-original", "update-models-1")
	selections := []*api.ModelSelection{{Slot: "chat", ModelId: "model-new"}, {Slot: "embedding", ModelId: "embedding-new"}}
	operation, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, selections))
	if err != nil {
		t.Fatalf("update models: %v", err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || operation.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_UPDATE_MODELS || operation.GetStage() != api.OperationStageEnum_OPERATION_STAGE_ENUM_SUCCEEDED || operation.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED {
		t.Fatalf("operation=%v", operation)
	}
	// Gateway mints exactly one key for the accepted model set and runtime.
	if gateway.mints != 1 || gateway.lastMint.GetWorkspaceId() != "workspace-original" || gateway.lastMint.GetTargetRuntimeInstanceId() != "runtime-original" || strings.Join(gateway.lastMint.GetModelIds(), ",") != "model-new,embedding-new" {
		t.Fatalf("gateway mint=%+v", gateway.lastMint)
	}
	// Fabric binds that exact Gateway delivery into the declared slot.
	if fabric.binds != 1 || fabric.lastBinding.GetKeyBindingId() != gateway.lastMint.GetWorkspaceId() && fabric.lastBinding.GetKeyBindingId() == "" || fabric.lastBinding.GetTargetSlot() != "gateway" || fabric.lastBinding.GetRuntimeInstanceId() != "runtime-original" || fabric.lastBinding.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef("workspace-original") {
		t.Fatalf("fabric bind=%+v", fabric.lastBinding)
	}
	// Serve carries the opaque binding and the expected applied version; Serve is
	// told the target version but never asked to mint a key.
	if serve.reloads != 1 || serve.lastReload.GetTargetVersion() != 1 || serve.lastReload.GetExpectedAppliedVersion() != 0 || serve.lastReload.GetManagedKeyBinding().GetKeyBindingId() == "" || serve.lastReload.GetManagedKeyBinding().GetSecretVersion() == "" || serve.lastReload.GetManagedKeyBinding().GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef("workspace-original") {
		t.Fatalf("serve reload=%+v", serve.lastReload)
	}
	// The Workspace advanced its own applied version only from Serve's confirmed
	// readback, and recorded the Gateway binding with the new version.
	if applied := appliedWorkspaceModelVersion(t, db); applied != 1 {
		t.Fatalf("applied version=%d, want the confirmed version", applied)
	}
	var binding string
	var observation string
	var selectionsRaw []byte
	if err := db.QueryRowContext(t.Context(), `SELECT gateway_key_binding_id,runtime_reload_observation,selections FROM workspace.model_configurations WHERE workspace_id='workspace-original' AND version=1`).Scan(&binding, &observation, &selectionsRaw); err != nil {
		t.Fatal(err)
	}
	if binding == "" || observation != "confirmed" {
		t.Fatalf("configuration binding=%q observation=%q", binding, observation)
	}
	stored, err := decodeModelSelections(selectionsRaw)
	if err != nil || len(stored) != 2 || stored[0].GetModelId() != "model-new" {
		t.Fatalf("stored selections=%v err=%v", stored, err)
	}
	// A first configuration has no superseded key to revoke.
	if gateway.revokes != 0 {
		t.Fatalf("revoked %d keys on a first configuration", gateway.revokes)
	}
	// The read surface now reports the applied configuration.
	configuration, err := service.GetWorkspaceModels(ctx, &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.GetVersion() != 1 || configuration.GetAppliedVersion() != 1 || configuration.GetStatus() != api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_APPLIED {
		t.Fatalf("read after update=%v", configuration)
	}
}

// TestUpdateWorkspaceModelsRequiresTheExpectedVersion proves an update whose
// expected version is not the Workspace's current configuration is refused before
// any Gateway, Fabric or Serve side effect.
func TestUpdateWorkspaceModelsRequiresTheExpectedVersion(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, fabric, serve := modelUpdateWorkspace(t, db, true)
	call, ctx := modelUpdateContext("tenant-original", "update-models-stale")
	_, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 4, []*api.ModelSelection{{Slot: "chat", ModelId: "model-new"}}))
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale expectation err=%v want a failed precondition", err)
	}
	if gateway.mints != 0 || fabric.binds != 0 || serve.reloads != 0 {
		t.Fatalf("stale update reached owners: gateway=%d fabric=%d serve=%d", gateway.mints, fabric.binds, serve.reloads)
	}
	if applied := appliedWorkspaceModelVersion(t, db); applied != 0 {
		t.Fatalf("stale update advanced the applied version to %d", applied)
	}
}

// TestUpdateWorkspaceModelsReplaysOneOperationForOneIdempotencyKey proves a lost
// response replays the original operation instead of minting a second key, a second
// Secret binding or a second configuration version.
func TestUpdateWorkspaceModelsReplaysOneOperationForOneIdempotencyKey(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, fabric, serve := modelUpdateWorkspace(t, db, true)
	call, ctx := modelUpdateContext("tenant-original", "update-models-replay")
	selections := []*api.ModelSelection{{Slot: "chat", ModelId: "model-new"}}
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
	if gateway.mints != 1 || fabric.binds != 1 || serve.reloads != 1 {
		t.Fatalf("replay repeated side effects: gateway=%d fabric=%d serve=%d", gateway.mints, fabric.binds, serve.reloads)
	}
	// A different body under the same key is a conflict, never a second write.
	conflict := &api.CallContext{RequestId: call.GetRequestId(), IdempotencyKey: call.GetIdempotencyKey(), ActorId: call.GetActorId(), SessionId: call.SessionId, Scope: call.GetScope()}
	if _, err = service.UpdateWorkspaceModels(ctx, modelUpdateRequest(conflict, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-other"}})); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("changed input under the same key err=%v want already exists", err)
	}
}

// TestUpdateWorkspaceModelsLeavesTheAppliedVersionUnconfirmedWhenAnOwnerDoesNotAnswer
// proves each unconfirmed step: an unanswered or refused Gateway/Fabric/Serve call
// leaves the Workspace applied version at its last confirmed value and records the
// configuration as not confirmed. The first update is confirmed normally, and the
// arranged failure is applied to a second update over version 1.
func TestUpdateWorkspaceModelsLeavesTheAppliedVersionUnconfirmedWhenAnOwnerDoesNotAnswer(t *testing.T) {
	cases := map[string]struct {
		arrange  func(*modelUpdateGateway, *modelUpdateFabric, *modelUpdateServe)
		rejected bool
	}{
		"gateway unavailable": {arrange: func(g *modelUpdateGateway, _ *modelUpdateFabric, _ *modelUpdateServe) {
			g.mintErr = status.Error(codes.Unavailable, "gateway lost the response")
		}},
		"fabric unavailable": {arrange: func(_ *modelUpdateGateway, f *modelUpdateFabric, _ *modelUpdateServe) {
			f.rebindErr = status.Error(codes.Unavailable, "fabric lost the response")
		}},
		"fabric mismatch": {arrange: func(_ *modelUpdateGateway, f *modelUpdateFabric, _ *modelUpdateServe) { f.rebindReject = true }, rejected: true},
		"serve unavailable": {arrange: func(_ *modelUpdateGateway, _ *modelUpdateFabric, s *modelUpdateServe) {
			s.reloadErr = status.Error(codes.Unavailable, "serve lost the response")
		}},
		"serve not succeeded": {arrange: func(_ *modelUpdateGateway, _ *modelUpdateFabric, s *modelUpdateServe) {
			s.serveStatus = api.OperationStatusEnum_OPERATION_STATUS_ENUM_AWAITING_CONFIRMATION
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db := runtimeDatabase(t)
			service, gateway, fabric, serve := modelUpdateWorkspace(t, db, true)
			call, ctx := modelUpdateContext("tenant-original", "update-models-first")
			if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}})); err != nil {
				t.Fatalf("first update: %v", err)
			}
			if applied := appliedWorkspaceModelVersion(t, db); applied != 1 {
				t.Fatalf("first update applied=%d want 1", applied)
			}
			tc.arrange(gateway, fabric, serve)
			next := &api.CallContext{RequestId: call.GetRequestId() + "-2", IdempotencyKey: call.GetIdempotencyKey() + "-2", ActorId: call.GetActorId(), SessionId: call.SessionId, Scope: call.GetScope()}
			failed, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(next, "workspace-original", 1, []*api.ModelSelection{{Slot: "chat", ModelId: "model-b"}}))
			if err != nil {
				t.Fatalf("failing update returned a transport error: %v", err)
			}
			if failed.GetStatus() == api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
				t.Fatalf("an unconfirmed step reported success: %v", failed)
			}
			if name != "fabric mismatch" && name != "serve not succeeded" && failed.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN {
				t.Fatalf("an unanswered owner was recorded as %v", failed.GetObservationResult())
			}
			if applied := appliedWorkspaceModelVersion(t, db); applied != 1 {
				t.Fatalf("an unconfirmed reload advanced the applied version to %d", applied)
			}
			// A failure that never recorded a configuration leaves the current version;
			// a failure after the row was written leaves that row unconfirmed, so the
			// read still reports the last confirmed version as applied.
			wantVersion := int64(2)
			if name == "gateway unavailable" {
				wantVersion = 1
			}
			if latest, _, err := service.latestModelConfigurationVersion(ctx, "workspace-original", appliedWorkspaceModelVersion(t, db)); err != nil || latest != wantVersion {
				t.Fatalf("latest configuration version=%d (%v), want %d", latest, err, wantVersion)
			}
			var observation string
			rowErr := db.QueryRowContext(t.Context(), `SELECT runtime_reload_observation FROM workspace.model_configurations WHERE workspace_id='workspace-original' AND version=2`).Scan(&observation)
			if name == "gateway unavailable" {
				if rowErr == nil {
					t.Fatal("a Gateway failure recorded a configuration version")
				}
			} else if rowErr != nil || observation == "confirmed" {
				t.Fatalf("the unconfirmed configuration row=%q err=%v", observation, rowErr)
			}
		})
	}
}

// TestUpdateWorkspaceModelsRefusesARuntimeWithoutTheGatewayCredential proves the
// update path is not fabricated for a runtime that declares no model-configuration
// capability: no key is minted and the applied version does not advance.
func TestUpdateWorkspaceModelsRefusesARuntimeWithoutTheGatewayCredential(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, fabric, serve := modelUpdateWorkspace(t, db, false)
	call, ctx := modelUpdateContext("tenant-original", "update-models-no-gateway")
	if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-new"}})); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("runtime without the Gateway credential err=%v want a failed precondition", err)
	}
	if gateway.mints != 0 || fabric.binds != 0 || serve.reloads != 0 {
		t.Fatalf("a runtime without the capability reached owners: gateway=%d fabric=%d serve=%d", gateway.mints, fabric.binds, serve.reloads)
	}
}

// TestUpdateWorkspaceModelsAdmitsOnlyTheOwningTenant proves an update is refused
// for another tenant and for a denied policy decision, before any side effect.
func TestUpdateWorkspaceModelsAdmitsOnlyTheOwningTenant(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, _, _ := modelUpdateWorkspace(t, db, true)
	call, ctx := modelUpdateContext("tenant-other", "update-models-other-tenant")
	if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-new"}})); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("another tenant err=%v want permission denied", err)
	}
	service.Auth = ownerservice.NewAuthorizer(ownerservice.OwnerWorkspace, &modelReadIdentity{denial: status.Error(codes.PermissionDenied, "admin role required")})
	call, ctx = modelUpdateContext("tenant-original", "update-models-denied")
	if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-new"}})); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("denied policy err=%v want permission denied", err)
	}
	if gateway.mints != 0 {
		t.Fatalf("a refused update minted %d keys", gateway.mints)
	}
}

// TestUpdateWorkspaceModelsRebindsTheSecondConfiguration proves the merged
// replacement path end to end: the first configuration binds initially through
// Fabric BindSecret, the second configuration that changes the managed key rebinds
// through Fabric RebindSecret against the exact predecessor the first recorded,
// Serve applies and reads back the new model version, and only then does the
// Workspace advance its applied version and revoke the predecessor Gateway key.
func TestUpdateWorkspaceModelsRebindsTheSecondConfiguration(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, fabric, serve := modelUpdateWorkspace(t, db, true)
	call, ctx := modelUpdateContext("tenant-original", "rebind-first")
	first, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}}))
	if err != nil || first.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("first update=%v %v", first, err)
	}
	if fabric.binds != 1 || fabric.rebinds != 0 {
		t.Fatalf("first configuration binds=%d rebinds=%d, want the initial bind only", fabric.binds, fabric.rebinds)
	}
	firstPredecessor := fabric.active
	second := &api.CallContext{RequestId: call.GetRequestId() + "-2", IdempotencyKey: call.GetIdempotencyKey() + "-2", ActorId: call.GetActorId(), SessionId: call.SessionId, Scope: call.GetScope()}
	operation, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(second, "workspace-original", 1, []*api.ModelSelection{{Slot: "chat", ModelId: "model-b"}}))
	if err != nil {
		t.Fatalf("second update: %v", err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("second update=%v", operation)
	}
	// The replacement named the exact predecessor the first configuration recorded.
	if fabric.rebinds != 1 || fabric.lastRebind.GetExpectedCurrentSecretBindingId() != firstPredecessor {
		t.Fatalf("rebind=%+v, want the first binding %q as predecessor", fabric.lastRebind, firstPredecessor)
	}
	if fabric.lastRebind.GetKeyBindingId() != "gateway-key-model-b" || fabric.lastRebind.GetFingerprint() == "" || fabric.lastRebind.GetTargetSlot() != "gateway" {
		t.Fatalf("rebind details=%+v", fabric.lastRebind)
	}
	// Serve carried the replacement generation, not the predecessor.
	if serve.reloads != 2 || serve.lastReload.GetManagedKeyBinding().GetSecretVersion() != "vmodel-b" {
		t.Fatalf("serve reloads=%d binding=%+v", serve.reloads, serve.lastReload.GetManagedKeyBinding())
	}
	// The predecessor key is revoked only now, and it is the first configuration's key.
	if gateway.mints != 2 || gateway.revokes != 1 || gateway.lastRevoke.GetKeyBindingId() != "gateway-key-model-a" || gateway.lastRevoke.GetWorkspaceId() != "workspace-original" {
		t.Fatalf("superseded key rotation: mints=%d revokes=%d last=%+v", gateway.mints, gateway.revokes, gateway.lastRevoke)
	}
	if applied := appliedWorkspaceModelVersion(t, db); applied != 2 {
		t.Fatalf("applied version=%d want 2", applied)
	}
}

// TestUpdateWorkspaceModelsRefusesARebindFabricDidNotConfirm proves fail-closed at
// the replacement readback: a Fabric replacement whose readback does not name the
// expected predecessor (or a changed fingerprint) is refused before Serve, the
// applied version stays at the last confirmed one, and the predecessor Gateway key
// is not revoked.
func TestUpdateWorkspaceModelsRefusesARebindFabricDidNotConfirm(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, fabric, serve := modelUpdateWorkspace(t, db, true)
	call, ctx := modelUpdateContext("tenant-original", "rebind-reject-first")
	if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}})); err != nil {
		t.Fatalf("first update: %v", err)
	}
	fabric.rebindReject = true
	second := &api.CallContext{RequestId: call.GetRequestId() + "-2", IdempotencyKey: call.GetIdempotencyKey() + "-2", ActorId: call.GetActorId(), SessionId: call.SessionId, Scope: call.GetScope()}
	operation, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(second, "workspace-original", 1, []*api.ModelSelection{{Slot: "chat", ModelId: "model-b"}}))
	if err != nil {
		t.Fatalf("second update: %v", err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION || operation.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED {
		t.Fatalf("a mismatched rebind readback was not refused: %v", operation)
	}
	if serve.reloads != 1 || gateway.revokes != 0 {
		t.Fatalf("a refused rebind reached Serve (%d) or revoked a key (%d)", serve.reloads, gateway.revokes)
	}
	if applied := appliedWorkspaceModelVersion(t, db); applied != 1 {
		t.Fatalf("a refused rebind advanced the applied version to %d", applied)
	}
}

// TestUpdateWorkspaceModelsKeepsThePredecessorKeyWhenRebindFails proves that a
// Fabric replacement that fails outright leaves the predecessor Gateway key live:
// the predecessor is revoked only after Serve confirms the new runtime version, so a
// failed replacement never strands the running runtime without its key.
func TestUpdateWorkspaceModelsKeepsThePredecessorKeyWhenRebindFails(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, fabric, serve := modelUpdateWorkspace(t, db, true)
	call, ctx := modelUpdateContext("tenant-original", "rebind-fail-first")
	if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}})); err != nil {
		t.Fatalf("first update: %v", err)
	}
	fabric.rebindErr = status.Error(codes.Unavailable, "the approved Secret store is unavailable")
	second := &api.CallContext{RequestId: call.GetRequestId() + "-2", IdempotencyKey: call.GetIdempotencyKey() + "-2", ActorId: call.GetActorId(), SessionId: call.SessionId, Scope: call.GetScope()}
	operation, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(second, "workspace-original", 1, []*api.ModelSelection{{Slot: "chat", ModelId: "model-b"}}))
	if err != nil {
		t.Fatalf("second update: %v", err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION || operation.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN {
		t.Fatalf("an unanswered rebind must leave the outcome unknown: %v", operation)
	}
	if serve.reloads != 1 || gateway.revokes != 0 {
		t.Fatalf("a failed rebind reached Serve (%d) or revoked the predecessor key (%d)", serve.reloads, gateway.revokes)
	}
	if applied := appliedWorkspaceModelVersion(t, db); applied != 1 {
		t.Fatalf("a failed rebind advanced the applied version to %d", applied)
	}
}

// TestUpdateWorkspaceModelsValidatesTheSelections proves a selection set that names
// no model, an empty slot or a duplicate slot is refused before any side effect.
func TestUpdateWorkspaceModelsValidatesTheSelections(t *testing.T) {
	for name, selections := range map[string][]*api.ModelSelection{
		"none":           {},
		"no model":       {{Slot: "chat"}},
		"no slot":        {{ModelId: "model-a"}},
		"duplicate slot": {{Slot: "chat", ModelId: "model-a"}, {Slot: "chat", ModelId: "model-b"}},
	} {
		t.Run(name, func(t *testing.T) {
			db := runtimeDatabase(t)
			service, gateway, fabric, serve := modelUpdateWorkspace(t, db, true)
			call, ctx := modelUpdateContext("tenant-original", "update-models-invalid")
			if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, selections)); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("invalid selections err=%v want invalid argument", err)
			}
			if gateway.mints != 0 || fabric.binds != 0 || serve.reloads != 0 {
				t.Fatal("invalid selections reached owners")
			}
		})
	}
}
