package launch

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

	"google.golang.org/genproto/googleapis/rpc/errdetails"
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

// modelUpdateGateway is the GatewayCoordination fixture for the update path. It
// mints one managed key per call for the exact model set and records how many keys
// it issued and revoked, so a replay can be told from a second key.
type modelUpdateGateway struct {
	api.GatewayCoordinationClient
	mints, revokes int
	lastMint       *api.ManagedKeyCommand
	lastRevoke     *api.ManagedKeyRevoke
	mintErr        error
	mintBinding    *api.ManagedKeyBinding
	revokeErr      error
}

func (g *modelUpdateGateway) CreateManagedKey(_ context.Context, r *api.ManagedKeyCommand, _ ...grpc.CallOption) (*api.ManagedKeyBinding, error) {
	g.mints++
	g.lastMint = proto.Clone(r).(*api.ManagedKeyCommand)
	if g.mintErr != nil {
		return nil, g.mintErr
	}
	if g.mintBinding != nil {
		return proto.Clone(g.mintBinding).(*api.ManagedKeyBinding), nil
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
	reloads            int
	lastReload         *api.RuntimeReloadCommand
	reloadErr          error
	serveStatus        api.OperationStatusEnum
	operationNil       bool
	beforeReloadAnswer func(context.Context, *api.RuntimeReloadCommand) error
}

func (s *modelUpdateServe) ReloadModels(ctx context.Context, r *api.RuntimeReloadCommand, _ ...grpc.CallOption) (*api.Operation, error) {
	s.reloads++
	s.lastReload = proto.Clone(r).(*api.RuntimeReloadCommand)
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

// seedLaunchManagedKeyBinding records on the Workspace's launch operation the opaque
// Gateway/Fabric binding the launch established for a declared Gateway credential, so
// the update path can name it as the predecessor it replaces.
func seedLaunchManagedKeyBinding(t *testing.T, db *sql.DB, secretBindingID, keyBindingID, runtimeInstanceID string) {
	t.Helper()
	var raw []byte
	if err := db.QueryRowContext(t.Context(), `SELECT COALESCE(result,'{}'::jsonb) FROM workspace.operations WHERE resource_id='workspace-original' AND kind='create_workspace'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(raw, &result) != nil {
		t.Fatal("launch result is invalid")
	}
	binding, err := json.Marshal(map[string]string{"keyBindingId": keyBindingID, "secretDeliveryReference": "opl-gateway-workspace-original", "fingerprint": "sha256:" + strings.Repeat("ab", 32), "targetSlot": "gateway", "secretBindingId": secretBindingID, "secretVersion": "vlaunch"})
	if err != nil {
		t.Fatal(err)
	}
	result["managedKeyBinding"] = binding
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(t.Context(), `UPDATE workspace.operations SET result=$1 WHERE resource_id='workspace-original' AND kind='create_workspace'`, encoded); err != nil {
		t.Fatal(err)
	}
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

// TestUpdateWorkspaceModelsReportsAnUnconfirmedRevocationToTheCustomerRead proves
// the customer read never presents an unconfirmed key revocation as applied. The
// runtime configuration really was applied - the architecture revokes the predecessor
// only after that readback - so the Workspace's applied version has advanced and the
// read keeps reporting it. The revocation itself is unresolved and the predecessor key
// is still live, so the status must carry that: APPLIED would present an operation
// nobody will ever finish as a finished one, and hide the one key the customer cannot
// see.
func TestUpdateWorkspaceModelsReportsAnUnconfirmedRevocationToTheCustomerRead(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, fabric, serve := modelUpdateWorkspace(t, db, true)
	call, ctx := modelUpdateContext("tenant-original", "revocation-unconfirmed-first")
	if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}})); err != nil {
		t.Fatalf("first update: %v", err)
	}
	// The only production issuer refuses revocation, so the predecessor key stays
	// live with its own model allowlist while the runtime has already moved.
	gateway.revokeErr = status.Error(codes.Unimplemented, "no approved Sub2API service revocation endpoint is configured")
	second := &api.CallContext{RequestId: call.GetRequestId() + "-2", IdempotencyKey: call.GetIdempotencyKey() + "-2", ActorId: call.GetActorId(), SessionId: call.SessionId, Scope: call.GetScope()}
	operation, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(second, "workspace-original", 1, []*api.ModelSelection{{Slot: "chat", ModelId: "model-b"}}))
	if err != nil {
		t.Fatalf("second update: %v", err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION || gateway.revokes != 1 || gateway.lastRevoke.GetKeyBindingId() != "gateway-key-model-a" {
		t.Fatalf("a refused revocation did not leave the update unresolved: %v revokes=%d last=%+v", operation, gateway.revokes, gateway.lastRevoke)
	}
	// The reload was genuinely applied and confirmed before the revoke was attempted,
	// so that fact is real and the read must not hide it either.
	if applied := appliedWorkspaceModelVersion(t, db); applied != 2 || fabric.rebinds != 1 || serve.reloads != 2 {
		t.Fatalf("applied version=%d rebinds=%d reloads=%d", applied, fabric.rebinds, serve.reloads)
	}
	configuration, err := service.GetWorkspaceModels(ctx, &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.GetVersion() != 2 || configuration.GetAppliedVersion() != 2 || configuration.GetOperationId() != operation.GetOperationId() {
		t.Fatalf("the confirmed configuration is not reported: %v", configuration)
	}
	if configuration.GetStatus() != api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_NEEDS_ATTENTION {
		t.Fatalf("an unconfirmed key revocation is presented as %v", configuration.GetStatus())
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

// TestUpdateWorkspaceModelsUsesTheLatestConfirmedBindingAfterRevokeNeedsAttention
// proves that a confirmed Serve/Fabric replacement remains the active predecessor
// even when the later Gateway revocation cannot be confirmed. A subsequent update
// must continue from that replacement binding rather than replaying an older one.
func TestUpdateWorkspaceModelsBlocksAfterRevokeNeedsAttention(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, fabric, _ := modelUpdateWorkspace(t, db, true)
	call, ctx := modelUpdateContext("tenant-original", "revoke-recovery-first")
	first, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}}))
	if err != nil || first.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("first update=%v %v", first, err)
	}
	firstBinding := fabric.active

	gateway.revokeErr = status.Error(codes.Unimplemented, "revocation authority is unavailable")
	second := &api.CallContext{RequestId: call.GetRequestId() + "-2", IdempotencyKey: call.GetIdempotencyKey() + "-2", ActorId: call.GetActorId(), SessionId: call.SessionId, Scope: call.GetScope()}
	attention, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(second, "workspace-original", 1, []*api.ModelSelection{{Slot: "chat", ModelId: "model-b"}}))
	if err != nil || attention.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION {
		t.Fatalf("revoke-unconfirmed update=%v %v", attention, err)
	}
	secondBinding := fabric.active
	if secondBinding == "" || secondBinding == firstBinding {
		t.Fatalf("confirmed replacement binding=%q, first=%q", secondBinding, firstBinding)
	}
	if applied := appliedWorkspaceModelVersion(t, db); applied != 2 {
		t.Fatalf("applied version after confirmed replacement=%d want 2", applied)
	}

	// The external refusal is cleared, but the accepted closeout is still unresolved.
	// A later update must block before creating another key rather than leaving the
	// launch-era key live behind an additional model generation.
	gateway.revokeErr = nil
	third := &api.CallContext{RequestId: call.GetRequestId() + "-3", IdempotencyKey: call.GetIdempotencyKey() + "-3", ActorId: call.GetActorId(), SessionId: call.SessionId, Scope: call.GetScope()}
	if _, err = service.UpdateWorkspaceModels(ctx, modelUpdateRequest(third, "workspace-original", 2, []*api.ModelSelection{{Slot: "chat", ModelId: "model-c"}})); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("unresolved revoke retry err=%v want failed precondition", err)
	}
	if gateway.mints != 2 || fabric.rebinds != 1 {
		t.Fatalf("blocked retry created side effects: mints=%d rebinds=%d", gateway.mints, fabric.rebinds)
	}
}

// TestUpdateWorkspaceModelsBlocksAfterServeRejectsAConfirmedFabricReplacement
// proves a failed Serve apply does not make Workspace reuse an older Fabric
// predecessor. The next model update must stop before minting a new Gateway key
// until the unresolved replacement has been reconciled.
func TestUpdateWorkspaceModelsBlocksAfterServeRejectsAConfirmedFabricReplacement(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, fabric, serve := modelUpdateWorkspace(t, db, true)
	call, ctx := modelUpdateContext("tenant-original", "serve-recovery-first")
	first, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}}))
	if err != nil || first.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("first update=%v %v", first, err)
	}

	serve.reloadErr = status.Error(codes.Unavailable, "Serve rejected the replacement")
	second := &api.CallContext{RequestId: call.GetRequestId() + "-2", IdempotencyKey: call.GetIdempotencyKey() + "-2", ActorId: call.GetActorId(), SessionId: call.SessionId, Scope: call.GetScope()}
	attention, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(second, "workspace-original", 1, []*api.ModelSelection{{Slot: "chat", ModelId: "model-b"}}))
	if err != nil || attention.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION {
		t.Fatalf("Serve-rejected update=%v %v", attention, err)
	}
	mintsBeforeBlockedRetry := gateway.mints
	rebindsBeforeBlockedRetry := fabric.rebinds

	serve.reloadErr = nil
	third := &api.CallContext{RequestId: call.GetRequestId() + "-3", IdempotencyKey: call.GetIdempotencyKey() + "-3", ActorId: call.GetActorId(), SessionId: call.SessionId, Scope: call.GetScope()}
	if _, err = service.UpdateWorkspaceModels(ctx, modelUpdateRequest(third, "workspace-original", 2, []*api.ModelSelection{{Slot: "chat", ModelId: "model-c"}})); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("unresolved replacement retry err=%v want failed precondition", err)
	}
	if gateway.mints != mintsBeforeBlockedRetry || fabric.rebinds != rebindsBeforeBlockedRetry {
		t.Fatalf("blocked retry created side effects: mints=%d/%d rebinds=%d/%d", gateway.mints, mintsBeforeBlockedRetry, fabric.rebinds, rebindsBeforeBlockedRetry)
	}
}

// TestUpdateWorkspaceModelsRefusesASecondAcceptedUpdateWhileOneIsRunning proves
// different idempotency keys cannot reserve the same configuration version and
// race into cross-owner side effects.
func TestUpdateWorkspaceModelsRefusesASecondAcceptedUpdateWhileOneIsRunning(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, _, _ := modelUpdateWorkspace(t, db, true)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO workspace.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,request_id,accepted_input,observation_result)
		VALUES('op-models-pending','tenant-original','actor-original','update_models','workspace-original','running','key','request-pending','{}'::jsonb,'unknown')`); err != nil {
		t.Fatal(err)
	}
	call, ctx := modelUpdateContext("tenant-original", "update-models-concurrent")
	if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}})); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("concurrent accepted update err=%v want failed precondition", err)
	}
	if gateway.mints != 0 {
		t.Fatalf("a concurrent update minted %d keys", gateway.mints)
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

// TestUpdateWorkspaceModelsRebindsTheLaunchEstablishedBinding proves the fixed
// connection: a runtime whose declared Gateway credential was delivered at launch
// (Workspace CreateManagedKey + Fabric BindSecret, recorded on the launch result)
// makes the first UpdateWorkspaceModels name that launch binding as its RebindSecret
// predecessor, so the first model change replaces the launch key instead of colliding
// with it.
func TestUpdateWorkspaceModelsRebindsTheLaunchEstablishedBinding(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, fabric, serve := modelUpdateWorkspace(t, db, true)
	// The launch already established the Workspace's Gateway key binding for the
	// runtime's gateway purpose and recorded its Fabric binding id.
	seedLaunchManagedKeyBinding(t, db, "sbx_launch_binding", "gateway-key-launch", "runtime-original")
	fabric.rows = map[string]*api.SecretBindingReadback{
		"runtime-original:gateway": {SecretBindingId: "sbx_launch_binding", RuntimeInstanceId: "runtime-original", Fingerprint: "sha256:" + strings.Repeat("ab", 32), Version: "vlaunch", Outcome: api.Observation_OBSERVATION_CONFIRMED},
	}
	fabric.active = "sbx_launch_binding"
	call, ctx := modelUpdateContext("tenant-original", "launch-binding-rebind")
	operation, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}}))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("first update=%v", operation)
	}
	// The first update rebound against the launch binding rather than calling
	// BindSecret for the same runtime purpose.
	if fabric.rebinds != 1 || fabric.binds != 0 {
		t.Fatalf("first update binds=%d rebinds=%d, want the launch predecessor rebind", fabric.binds, fabric.rebinds)
	}
	if fabric.lastRebind.GetExpectedCurrentSecretBindingId() != "sbx_launch_binding" {
		t.Fatalf("the first update named predecessor %q, want the launch binding", fabric.lastRebind.GetExpectedCurrentSecretBindingId())
	}
	// The launch Gateway key is revoked after Serve confirms the new version.
	if gateway.revokes != 1 || gateway.lastRevoke.GetKeyBindingId() != "gateway-key-launch" {
		t.Fatalf("the launch key was not revoked: revokes=%d last=%+v", gateway.revokes, gateway.lastRevoke)
	}
	if applied := appliedWorkspaceModelVersion(t, db); applied != 1 {
		t.Fatalf("applied version=%d want 1", applied)
	}
	if serve.reloads != 1 {
		t.Fatalf("serve reloads=%d want 1", serve.reloads)
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

// TestUpdateWorkspaceModelsNativeFailureReadback drives the registered Workspace
// producer, PostgreSQL row and registered OwnerOperations.Read over authenticated
// loopback gRPC. Only downstream coordination and CloudIdentity decisions are
// fixtures; neither the persisted operation nor its public reader is stubbed.
func TestUpdateWorkspaceModelsNativeFailureReadback(t *testing.T) {
	cases := []struct {
		name        string
		arrange     func(*modelUpdateGateway, *modelUpdateFabric, *modelUpdateServe)
		first       bool
		stage       api.OperationStageEnum
		observation api.OperationObservationResultEnum
		code        api.ErrorCodeEnum
		applied     int64
	}{
		{"key unavailable", func(g *modelUpdateGateway, _ *modelUpdateFabric, _ *modelUpdateServe) {
			g.mintErr = status.Error(codes.Unavailable, "synthetic Gateway response lost")
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE, 0},
		{"key precondition refused", func(g *modelUpdateGateway, _ *modelUpdateFabric, _ *modelUpdateServe) {
			g.mintErr = status.Error(codes.FailedPrecondition, "synthetic key admission refused")
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_VALIDATION_FAILED, 0},
		{"key typed model refusal", func(g *modelUpdateGateway, _ *modelUpdateFabric, _ *modelUpdateServe) {
			g.mintErr = owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "synthetic model refusal"), api.ErrorCodeEnum_ERROR_CODE_ENUM_MODEL_NOT_ALLOWED)
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_MODEL_NOT_ALLOWED, 0},
		{"bind unavailable", func(_ *modelUpdateGateway, f *modelUpdateFabric, _ *modelUpdateServe) {
			f.bindErr = status.Error(codes.Unavailable, "synthetic Fabric response lost")
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_CONFIGURATION, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE, 0},
		{"rebind unavailable", func(_ *modelUpdateGateway, f *modelUpdateFabric, _ *modelUpdateServe) {
			f.rebindErr = status.Error(codes.Unavailable, "synthetic Fabric replacement response lost")
		}, true, api.OperationStageEnum_OPERATION_STAGE_ENUM_CONFIGURATION, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE, 1},
		{"bind readback refused", func(_ *modelUpdateGateway, f *modelUpdateFabric, _ *modelUpdateServe) { f.rejected = true }, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_CONFIGURATION, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_VALIDATION_FAILED, 0},
		{"reload precondition refused", func(_ *modelUpdateGateway, _ *modelUpdateFabric, s *modelUpdateServe) {
			s.reloadErr = status.Error(codes.FailedPrecondition, "synthetic reload admission refused")
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_VALIDATION_FAILED, 0},
		{"reload unavailable", func(_ *modelUpdateGateway, _ *modelUpdateFabric, s *modelUpdateServe) {
			s.reloadErr = status.Error(codes.Unavailable, "synthetic Serve response lost")
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE, 0},
		{"reload typed access refusal", func(_ *modelUpdateGateway, _ *modelUpdateFabric, s *modelUpdateServe) {
			s.reloadErr = owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "synthetic unsupported entry"), api.ErrorCodeEnum_ERROR_CODE_ENUM_APP_ACCESS_UNAVAILABLE)
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_APP_ACCESS_UNAVAILABLE, 0},
		{"revoke unimplemented", func(g *modelUpdateGateway, _ *modelUpdateFabric, _ *modelUpdateServe) {
			g.revokeErr = status.Error(codes.Unimplemented, "synthetic revocation capability absent")
		}, true, api.OperationStageEnum_OPERATION_STAGE_ENUM_VERIFICATION, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_OWNER_CAPABILITY_UNAVAILABLE, 2},
		{"key typed unknown", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			g.mintErr = owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "synthetic external key outcome unconfirmed"), api.ErrorCodeEnum_ERROR_CODE_ENUM_EXTERNAL_OUTCOME_UNKNOWN)
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_EXTERNAL_OUTCOME_UNKNOWN, 0},
		{"reload typed unknown", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			s.reloadErr = owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "synthetic runtime outcome unconfirmed"), api.ErrorCodeEnum_ERROR_CODE_ENUM_EXTERNAL_OUTCOME_UNKNOWN)
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_EXTERNAL_OUTCOME_UNKNOWN, 0},
		{"revoke typed unknown", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			g.revokeErr = owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "synthetic retirement outcome unconfirmed"), api.ErrorCodeEnum_ERROR_CODE_ENUM_EXTERNAL_OUTCOME_UNKNOWN)
		}, true, api.OperationStageEnum_OPERATION_STAGE_ENUM_VERIFICATION, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_EXTERNAL_OUTCOME_UNKNOWN, 2},
		{"key typed unspecified", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			g.mintErr = modelUpdateNativeErrorInfo(t, "UNSPECIFIED")
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 0},
		{"reload typed numeric undefined", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			s.reloadErr = modelUpdateNativeErrorInfo(t, "2147483647")
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 0},
		{"reload typed reason undeclared", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			s.reloadErr = modelUpdateNativeErrorInfo(t, "UNDECLARED_OWNER_REASON")
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 0},
		{"bind active binding conflict", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			f.bindErr = status.Error(codes.AlreadyExists, "the runtime instance already holds an active Secret binding for this slot")
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_CONFIGURATION, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 0},
		{"rebind input conflict", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			f.rebindErr = status.Error(codes.AlreadyExists, "idempotency key has a different Secret replacement")
		}, true, api.OperationStageEnum_OPERATION_STAGE_ENUM_CONFIGURATION, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 1},
		{"reload original input conflict", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			s.reloadErr = status.Error(codes.AlreadyExists, "managed_key_unavailable: the reload input differs from its original command")
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 0},
		{"key typed idempotency conflict", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			g.mintErr = owneridentity.WithErrorCode(status.Error(codes.AlreadyExists, "idempotency key reused with a different managed key command"), api.ErrorCodeEnum_ERROR_CODE_ENUM_IDEMPOTENCY_CONFLICT)
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 0},
		{"key stored answer data loss", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			g.mintErr = status.Error(codes.DataLoss, "stored managed key command answer is invalid")
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 0},
		{"key invalid binding readback", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			g.mintBinding = &api.ManagedKeyBinding{KeyBindingId: "observed-key", WorkspaceId: "another-workspace"}
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 0},
		{"rebind stored readback data loss", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			f.rebindErr = status.Error(codes.DataLoss, "stored Secret rebind readback is invalid")
		}, true, api.OperationStageEnum_OPERATION_STAGE_ENUM_CONFIGURATION, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 1},
		{"reload snapshot data loss", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			s.reloadErr = status.Error(codes.DataLoss, "stored runtime reload snapshot is invalid")
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 0},
		{"reload effect without applied version", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			s.reloadErr = status.Error(codes.Internal, "the execution boundary reported no applied model configuration version")
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 0},
		{"reload aborted outcome", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			s.reloadErr = status.Error(codes.Aborted, "synthetic aborted acknowledgement without outcome readback")
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 0},
		{"key raw unauthenticated", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			g.mintErr = status.Error(codes.Unauthenticated, "synthetic downstream accepted grant is unauthenticated")
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 0},
		{"key typed synchronous unauthenticated", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			g.mintErr = owneridentity.WithErrorCode(status.Error(codes.Unauthenticated, "synthetic downstream identity failure"), api.ErrorCodeEnum_ERROR_CODE_ENUM_UNAUTHENTICATED)
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 0},
		{"key typed synchronous csrf", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			g.mintErr = owneridentity.WithErrorCode(status.Error(codes.PermissionDenied, "synthetic dependency synchronous csrf refusal"), api.ErrorCodeEnum_ERROR_CODE_ENUM_CSRF_INVALID)
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 0},
		{"key typed synchronous origin", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			g.mintErr = owneridentity.WithErrorCode(status.Error(codes.PermissionDenied, "synthetic dependency synchronous origin refusal"), api.ErrorCodeEnum_ERROR_CODE_ENUM_ORIGIN_REJECTED)
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 0},
		{"key typed synchronous idempotency required", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			g.mintErr = owneridentity.WithErrorCode(status.Error(codes.InvalidArgument, "synthetic dependency idempotency identity missing"), api.ErrorCodeEnum_ERROR_CODE_ENUM_IDEMPOTENCY_REQUIRED)
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 0},
		{"key typed synchronous uncertain", func(g *modelUpdateGateway, f *modelUpdateFabric, s *modelUpdateServe) {
			g.mintErr = owneridentity.WithErrorCode(status.Error(codes.Unavailable, "synthetic transport lost synchronous conflict outcome"), api.ErrorCodeEnum_ERROR_CODE_ENUM_IDEMPOTENCY_CONFLICT)
		}, false, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := runtimeDatabase(t)
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
			writer, reader := api.NewWorkspaceProductServiceClient(conn), api.NewOwnerOperationsClient(conn)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			expected := int64(0)
			if tc.first {
				call, _ := modelUpdateContext("tenant-original", "native-first")
				first, err := writer.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}}))
				if err != nil || first.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
					t.Fatalf("setup update: %v %v", first, err)
				}
				expected = 1
			}
			tc.arrange(gateway, fabric, serve)
			call, _ := modelUpdateContext("tenant-original", "native-failure")
			request := modelUpdateRequest(call, "workspace-original", expected, []*api.ModelSelection{{Slot: "chat", ModelId: "model-b"}})
			put, err := writer.UpdateWorkspaceModels(ctx, request)
			operationID := put.GetOperationId()
			if err != nil || operationID == "" {
				t.Errorf("registered update returned no readable operation: %v %v", put, err)
				// Recover the actually accepted identity from its own request, so a
				// failure response cannot hide a stranded running operation.
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
				if operation.GetOperationId() != row.ID || operation.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE || operation.GetResourceId() != "workspace-original" || operation.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_UPDATE_MODELS || operation.GetStage() != tc.stage || operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION || operation.GetObservationResult() != tc.observation || operation.ErrorCode == nil || operation.GetErrorCode() != tc.code || operation.GetPollAfterSeconds() != 5 {
					t.Errorf("%s does not report the persisted failure: %v; want stage=%s observation=%s code=%s", name, operation, tc.stage, tc.observation, tc.code)
				}
				if _, err := publicjson.Marshal(operation); err != nil {
					t.Errorf("%s public JSON: %v", name, err)
				}
			}
			if appliedWorkspaceModelVersion(t, db) != tc.applied {
				t.Errorf("applied version differs from confirmed effect: want %d", tc.applied)
			}
			if row.Status != "needs_attention" || row.Observation != strings.ToLower(strings.TrimPrefix(tc.observation.String(), "OPERATION_OBSERVATION_RESULT_ENUM_")) {
				t.Errorf("accepted operation was stranded or lost its observation: status=%s observation=%s", row.Status, row.Observation)
			}
			if tc.stage != api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY {
				var persistedObservation string
				if err := db.QueryRowContext(ctx, `SELECT runtime_reload_observation FROM workspace.model_configurations WHERE operation_id=$1`, row.ID).Scan(&persistedObservation); err != nil {
					t.Fatal(err)
				}
				wantObservation := row.Observation
				if tc.stage == api.OperationStageEnum_OPERATION_STAGE_ENUM_VERIFICATION {
					wantObservation = "confirmed" // apply succeeded before retirement failed
				}
				if persistedObservation != wantObservation {
					t.Errorf("configuration observation=%s want %s", persistedObservation, wantObservation)
				}
				configuration, err := writer.GetWorkspaceModels(ctx, &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: "workspace-original"})
				if err != nil || configuration.GetOperationId() != row.ID || configuration.GetAppliedVersion() != tc.applied || configuration.GetStatus() != api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_NEEDS_ATTENTION {
					t.Errorf("registered configuration read lost the original operation/outcome: %v %v", configuration, err)
				}
			}
			mints, binds, rebinds, reloads, revokes := gateway.mints, fabric.binds, fabric.rebinds, serve.reloads, gateway.revokes
			replay, err := writer.UpdateWorkspaceModels(ctx, request)
			if err != nil || !proto.Equal(put, replay) {
				t.Errorf("same-intent replay: %v %v, original %v", replay, err, put)
			}
			if mints != gateway.mints || binds != fabric.binds || rebinds != fabric.rebinds || reloads != serve.reloads || revokes != gateway.revokes {
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
			wrongPeer := modelUpdateNativeDial(t, workspaceAddress, owneridentity.Workspace.Service(), owneridentity.Workspace.Service())
			if _, err := api.NewOwnerOperationsClient(wrongPeer).Read(ctx, &api.OwnerOperationRequest{Context: call, OperationId: row.ID}); status.Code(err) != codes.PermissionDenied {
				t.Errorf("reader did not require the admitted Console BFF service identity: %v", err)
			}
		})
	}
}

// modelUpdateNativeErrorInfo sends malformed canonical metadata through the real
// producer boundary. WithErrorCode intentionally cannot construct these claims.
func modelUpdateNativeErrorInfo(t *testing.T, reason string) error {
	t.Helper()
	st, err := status.New(codes.FailedPrecondition, "synthetic invalid owner reason").WithDetails(&errdetails.ErrorInfo{Domain: owneridentity.ErrorDomain, Reason: reason})
	if err != nil {
		t.Fatal(err)
	}
	return st.Err()
}

// TestUpdateWorkspaceModelsNativeAppliedVersionCAS holds the same accepted intent
// and reload response constant, changing only the owner's applied version before
// confirmation. It exercises the registered producer, real transaction and
// registered operation readback, including predecessor-key retirement ordering.
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
			service, gateway, fabric, serve := modelUpdateWorkspace(t, db, true)
			seedLaunchManagedKeyBinding(t, db, "sbx_launch_binding", "gateway-key-launch", "runtime-original")
			fabric.active = "sbx_launch_binding"
			fabric.rows = map[string]*api.SecretBindingReadback{"runtime-original:gateway": {
				SecretBindingId: "sbx_launch_binding", RuntimeInstanceId: "runtime-original", Fingerprint: "sha256:" + strings.Repeat("ab", 32), Version: "vlaunch", Outcome: api.Observation_OBSERVATION_CONFIRMED,
			}}
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
			writer, reader := api.NewWorkspaceProductServiceClient(conn), api.NewOwnerOperationsClient(conn)
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
			t.Logf("CAS captured=0 target=1 concurrent=%d applied=%d workspaceVersion=%d operation=%s state=%s observation=%s errorCode=%s configurationObservation=%s verificationEvidencePresent=%t revokes=%d", tc.concurrentApplied, applied, workspaceVersion, operationID, row.Status, row.Observation, row.ErrorCode, observation, evidence.Valid, gateway.revokes)
			if tc.concurrentApplied != 0 {
				if applied != tc.concurrentApplied || workspaceVersion != 2 {
					t.Errorf("stale callback changed newer owner facts: applied=%d version=%d want %d/2", applied, workspaceVersion, tc.concurrentApplied)
				}
				if observation != "unknown" || evidence.Valid {
					t.Errorf("failed confirmation transaction did not roll back: observation=%s evidence=%v", observation, evidence)
				}
				if gateway.revokes != 0 {
					t.Errorf("stale callback retired %d predecessor keys", gateway.revokes)
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
				if applied != 1 || workspaceVersion != 2 || observation != "confirmed" || !evidence.Valid || evidence.String != "workspace-model-configuration://"+configurationID+"/vmodel-b" {
					t.Errorf("adjacent happy confirmation did not commit atomically: applied=%d version=%d observation=%s evidence=%v", applied, workspaceVersion, observation, evidence)
				}
				if gateway.revokes != 1 || gateway.lastRevoke.GetKeyBindingId() != "gateway-key-launch" {
					t.Errorf("happy confirmation did not retire predecessor after apply: revokes=%d last=%v", gateway.revokes, gateway.lastRevoke)
				}
				if put.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || !proto.Equal(put, get) {
					t.Errorf("happy PUT/GET mismatch: %v / %v", put, get)
				}
			}
			if gateway.mints != 1 || fabric.binds != 0 || fabric.rebinds != 1 || serve.reloads != 1 {
				t.Errorf("unexpected dispatch counts mints=%d binds=%d rebinds=%d reloads=%d", gateway.mints, fabric.binds, fabric.rebinds, serve.reloads)
			}
			configuration, err := writer.GetWorkspaceModels(ctx, &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: "workspace-original"})
			if err != nil || configuration.GetOperationId() != operationID || configuration.GetAppliedVersion() != applied {
				t.Errorf("registered configuration read does not retain owner facts: %v %v", configuration, err)
			}
			wantRevokes := gateway.revokes
			replay, err := writer.UpdateWorkspaceModels(ctx, request)
			if err != nil || !proto.Equal(replay, get) || gateway.mints != 1 || fabric.rebinds != 1 || serve.reloads != 1 || gateway.revokes != wantRevokes {
				t.Errorf("CAS replay repeated effects or changed original identity: %v %v", replay, err)
			}
		})
	}
}
