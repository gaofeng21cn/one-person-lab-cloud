package launch

// This file owns the Workspace's model-configuration update: the one
// owner-separated operation that turns an accepted model intent into a
// confirmed, applied configuration. The owners stay separate on purpose -
// Workspace authorizes and versions the intent, Gateway owns and reads back the
// managed key and its approved model scope, and Serve performs the runtime Secret
// handover and reports the version the application itself read back. An ordinary
// model update keeps the exact same external key; only a separately authorized
// scope change may widen it. The applied column therefore advances only from
// Serve's confirmed readback, and the public request never carries a Gateway key
// binding.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
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

// UpdateWorkspaceModels applies one accepted model configuration to the
// Workspace's current runtime. A new configuration version is created for the
// new selections under the very same Gateway-managed key binding the Workspace
// already holds: the Gateway owner reads back that binding's approved model
// scope, and the update is refused before any effect when the selection is not
// already inside it. Serve alone performs the Secret handover and applies and
// reads back the publisher configuration. Only a confirmed readback advances the
// Workspace's own applied version, so an unanswered or refused reload never
// presents the requested configuration as the applied one.
func (s *Service) UpdateWorkspaceModels(ctx context.Context, r *api.UpdateWorkspaceModelsRpcRequest) (*api.Operation, error) {
	c, body := r.GetContext(), r.GetBody()
	if err := ownerservice.ValidateCallContext(ctx, c); err != nil {
		return nil, err
	}
	workspaceID := strings.TrimSpace(r.GetWorkspaceId())
	if workspaceID == "" || body == nil || c.GetIdempotencyKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace, body and idempotency key are required")
	}
	selections, err := validateModelSelections(body.GetSelections())
	if err != nil {
		return nil, err
	}
	if s.Gateway == nil || s.Serve == nil {
		return nil, status.Error(codes.Unavailable, "Gateway and Serve are required to update a model configuration")
	}
	tenant, appliedVersion, err := s.modelConfigurationWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if err = s.authorize(ctx, c, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEMODELS, workspaceResource(workspaceID), tenant); err != nil {
		return nil, err
	}
	launch, err := s.workspaceLaunch(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	revision, err := launch.revision()
	if err != nil {
		return nil, err
	}
	credential, declared := contracts.WorkspaceApplicationDeclaredCredential(revision, contracts.WorkspaceApplicationCredentialGatewayKey)
	if !declared {
		return nil, status.Errorf(codes.FailedPrecondition, "%s: the runtime declares no model-configuration capability", ReasonModelConfigurationUnavailable)
	}
	op, targetVersion, replay, err := s.acceptModelUpdate(ctx, c, workspaceID, body, tenant)
	if err != nil {
		return nil, err
	}
	if replay {
		return modelUpdateOperation(op)
	}
	// Admission serializes this Workspace's model updates, but the initial read
	// happened before that admission lock. Refresh the owner-owned applied fact so
	// Serve receives the version the accepted operation actually follows.
	_, appliedVersion, err = s.modelConfigurationWorkspace(ctx, workspaceID)
	if err != nil {
		return s.failModelUpdate(ctx, op, "admission", err)
	}
	return s.runModelUpdate(ctx, op, launch, credential, selections, appliedVersion, targetVersion)
}

// ReasonModelConfigurationUnavailable names a runtime whose frozen revision
// declares no installation Gateway credential, so no model configuration can be
// applied to it.
const ReasonModelConfigurationUnavailable = "model_configuration_unavailable"

// ReasonManagedKeyScopeExpansionRequired names a selection the Workspace's current
// managed key is not approved for. Widening a live key is a separately authorized
// external scope change, so the update is refused instead of minting a wider key.
const ReasonManagedKeyScopeExpansionRequired = "managed_key_scope_expansion_required"

// validateModelSelections restates and checks one accepted selection set: every
// slot names exactly one model, and no slot is named twice, so the key Gateway
// allowlists and the selections Serve applies describe the same model set.
func validateModelSelections(selections []*api.ModelSelection) ([]*api.ModelSelection, error) {
	if len(selections) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one model selection is required")
	}
	seen := map[string]bool{}
	out := make([]*api.ModelSelection, 0, len(selections))
	for _, selection := range selections {
		slot, model := strings.TrimSpace(selection.GetSlot()), strings.TrimSpace(selection.GetModelId())
		if slot == "" || model == "" {
			return nil, status.Error(codes.InvalidArgument, "every model selection requires a slot and a model id")
		}
		if seen[slot] {
			return nil, status.Errorf(codes.InvalidArgument, "model selection slot %q is named twice", slot)
		}
		seen[slot] = true
		out = append(out, &api.ModelSelection{Slot: slot, ModelId: model})
	}
	return out, nil
}

// modelConfigurationWorkspace reads the Workspace's own tenant and applied model
// configuration version. The applied version is the Workspace's column, which only
// a confirmed runtime readback advances.
func (s *Service) modelConfigurationWorkspace(ctx context.Context, workspaceID string) (string, int64, error) {
	var tenant string
	var applied int64
	if err := s.Store.DB().QueryRowContext(ctx, `SELECT tenant_id,model_configuration_version FROM workspace.workspaces WHERE id=$1`, workspaceID).Scan(&tenant, &applied); err != nil {
		return "", 0, dbError(err)
	}
	return tenant, applied, nil
}

// latestModelConfigurationVersion reports the version the caller's next write would
// follow: the highest recorded configuration, or the applied version when no change
// has been recorded yet. It also returns that recorded row, so the update path can
// revoke exactly the key binding it supersedes.
func (s *Service) latestModelConfigurationVersion(ctx context.Context, workspaceID string, appliedVersion int64) (int64, modelConfigurationRow, error) {
	latest, found, err := s.latestModelConfiguration(ctx, workspaceID)
	if err != nil {
		return 0, modelConfigurationRow{}, err
	}
	if !found {
		return appliedVersion, modelConfigurationRow{}, nil
	}
	return latest.version, latest, nil
}

// latestModelConfigurationTx reads the highest recorded configuration under the
// caller's transaction, so the write path decides the next version from the same
// locked snapshot it writes against.
func latestModelConfigurationTx(ctx context.Context, tx *sql.Tx, workspaceID string) (modelConfigurationRow, bool, error) {
	var row modelConfigurationRow
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT c.version,c.operation_id,c.runtime_reload_observation,c.selections,c.updated_at,o.status
		FROM workspace.model_configurations c
		JOIN workspace.operations o ON o.id=c.operation_id
		WHERE c.workspace_id=$1 ORDER BY c.version DESC LIMIT 1`, workspaceID).
		Scan(&row.version, &row.operationID, &row.observation, &raw, &row.updatedAt, &row.operationState)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	if err != nil {
		return row, false, dbError(err)
	}
	selections, err := decodeModelSelections(raw)
	if err != nil {
		return row, false, err
	}
	row.selections = selections
	return row, true, nil
}

// modelLaunch is the frozen launch fact the update path continues: the bounded
// grant the accepted order obtained, the runtime instance that order delivered and
// the immutable application source it was admitted against.
type modelLaunch struct {
	grantID           string
	runtimeInstanceID string
	source            *sourceRecord
	// keyBindingID is the launch-issued Gateway key binding for a runtime whose
	// revision declares the installation Gateway credential. Later model
	// configurations preserve this exact key; it is the identity the update reads
	// back from its owner rather than a predecessor to replace.
	keyBindingID string
}

func (l modelLaunch) revision() (contracts.WorkspaceApplicationRevision, error) {
	var zero contracts.WorkspaceApplicationRevision
	if l.source == nil {
		return zero, status.Error(codes.FailedPrecondition, "the Workspace has no confirmed launch source")
	}
	resolved, err := l.source.resolve()
	if err != nil {
		return zero, err
	}
	if resolved.DeploymentDescriptor == nil || resolved.DeploymentDescriptor.GetApplicationRevision() == nil {
		return zero, status.Error(codes.DataLoss, "stored launch source carries no application revision")
	}
	raw, err := publicjson.Marshal(resolved.DeploymentDescriptor.GetApplicationRevision())
	if err != nil {
		return zero, err
	}
	var revision contracts.WorkspaceApplicationRevision
	if json.Unmarshal(raw, &revision) != nil || contracts.ValidateWorkspaceApplicationRevision(revision) != nil {
		return zero, status.Error(codes.DataLoss, "stored launch application revision is invalid")
	}
	return revision, nil
}

// workspaceLaunch resolves the accepted launch of one Workspace: its original
// operation, the bounded grant that operation obtained and the exact runtime it
// delivered. A Workspace with no delivered runtime has nothing to apply a model
// configuration to, which is refused rather than invented.
func (s *Service) workspaceLaunch(ctx context.Context, workspaceID string) (modelLaunch, error) {
	var raw []byte
	err := s.Store.DB().QueryRowContext(ctx, `SELECT COALESCE(result,'{}'::jsonb) FROM workspace.operations
		WHERE resource_id=$1 AND kind='create_workspace' ORDER BY created_at DESC, id DESC LIMIT 1`, workspaceID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return modelLaunch{}, status.Error(codes.FailedPrecondition, "the Workspace has no accepted launch")
	}
	if err != nil {
		return modelLaunch{}, dbError(err)
	}
	var result orderResult
	if json.Unmarshal(raw, &result) != nil {
		return modelLaunch{}, status.Error(codes.DataLoss, "stored Workspace launch result is invalid")
	}
	if strings.TrimSpace(result.GrantID) == "" || len(result.RuntimeCommand) == 0 || len(result.ApplicationSource) == 0 {
		return modelLaunch{}, status.Error(codes.FailedPrecondition, "the Workspace has no delivered runtime for a model configuration")
	}
	command := &api.RuntimeDeployCommand{}
	if protojson.Unmarshal(result.RuntimeCommand, command) != nil || strings.TrimSpace(command.GetRuntimeInstanceId()) == "" || command.GetWorkspaceId() != workspaceID {
		return modelLaunch{}, status.Error(codes.DataLoss, "stored Workspace runtime command is invalid")
	}
	source := &sourceRecord{}
	if json.Unmarshal(result.ApplicationSource, source) != nil {
		return modelLaunch{}, status.Error(codes.DataLoss, "stored Workspace application source is invalid")
	}
	keyBindingID := ""
	if len(result.ManagedKeyBinding) > 0 {
		binding := &api.RuntimeManagedKeyBinding{}
		if protojson.Unmarshal(result.ManagedKeyBinding, binding) != nil {
			return modelLaunch{}, status.Error(codes.DataLoss, "stored launch managed key binding is invalid")
		}
		keyBindingID = strings.TrimSpace(binding.GetKeyBindingId())
	}
	return modelLaunch{grantID: result.GrantID, runtimeInstanceID: command.GetRuntimeInstanceId(), source: source, keyBindingID: keyBindingID}, nil
}

// acceptModelUpdate durably records the new configuration intent and its Operation
// before any cross-owner side effect, and reports whether an earlier identical
// request already owns that intent. A lost response therefore replays the original
// operation instead of minting a second key or a second configuration version.
func (s *Service) acceptModelUpdate(ctx context.Context, c *api.CallContext, workspaceID string, body *api.UpdateWorkspaceModelsRequest, tenant string) (ownerstore.Operation, int64, bool, error) {
	normalized, err := proto.MarshalOptions{Deterministic: true}.Marshal(body)
	if err != nil {
		return ownerstore.Operation{}, 0, false, status.Error(codes.InvalidArgument, "invalid model configuration input")
	}
	idem := ownerstore.IdempotencyInput{ID: id("idem_"), TenantScope: tenant, ActorScope: c.GetActorId(), OperationName: "updateWorkspaceModels", IdempotencyKey: c.GetIdempotencyKey(), RequestSHA256: ownerstore.HashRequestBody(normalized), ResponseStatus: 202}
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return ownerstore.Operation{}, 0, false, dbError(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenant+"/"+c.GetActorId()+"/updateWorkspaceModels/"+c.GetIdempotencyKey()); err != nil {
		return ownerstore.Operation{}, 0, false, dbError(err)
	}
	replay, found, err := s.Store.LookupIdempotency(ctx, tx, idem)
	if errors.Is(err, ownerstore.ErrIdempotencyConflict) {
		return ownerstore.Operation{}, 0, false, status.Error(codes.AlreadyExists, "idempotency key has different input")
	}
	if err != nil {
		return ownerstore.Operation{}, 0, false, dbError(err)
	}
	if found {
		tx.Rollback()
		op, err := s.Store.ReadOperation(ctx, replay.OperationID)
		if err != nil {
			return ownerstore.Operation{}, 0, false, dbError(err)
		}
		return op, 0, true, nil
	}
	// Re-check the expected version under the idempotency lock, so two concurrent
	// updates for one Workspace cannot both write the same configuration version.
	var applied int64
	if err = tx.QueryRowContext(ctx, `SELECT model_configuration_version FROM workspace.workspaces WHERE id=$1 FOR UPDATE`, workspaceID).Scan(&applied); err != nil {
		return ownerstore.Operation{}, 0, false, dbError(err)
	}
	// The Workspace row lock serializes admissions with different command keys.
	// Check pending work after acquiring it so a waiting transaction sees the
	// operation committed by the preceding admission before it creates a new one.
	var pending bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM workspace.operations
		WHERE resource_id=$1 AND kind='update_models' AND status NOT IN ('succeeded','failed','cancelled')
	)`, workspaceID).Scan(&pending); err != nil {
		return ownerstore.Operation{}, 0, false, dbError(err)
	}
	if pending {
		return ownerstore.Operation{}, 0, false, status.Error(codes.FailedPrecondition, "another Workspace model update is still unresolved")
	}
	recorded, found, err := latestModelConfigurationTx(ctx, tx, workspaceID)
	if err != nil {
		return ownerstore.Operation{}, 0, false, err
	}
	current := applied
	if found {
		current = recorded.version
	}
	if body.GetExpectedVersion() != current {
		return ownerstore.Operation{}, 0, false, status.Errorf(codes.FailedPrecondition, "Workspace model configuration is at version %d, not the expected %d", current, body.GetExpectedVersion())
	}
	targetVersion := current + 1
	opID := id("op_")
	accepted, err := json.Marshal(struct {
		ExpectedVersion int64                 `json:"expectedVersion"`
		Selections      []*api.ModelSelection `json:"selections"`
	}{ExpectedVersion: body.GetExpectedVersion(), Selections: body.GetSelections()})
	if err != nil {
		return ownerstore.Operation{}, 0, false, status.Error(codes.Internal, "Workspace model update cannot be encoded")
	}
	op, err := s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: opID, TenantID: tenant, ActorID: c.GetActorId(), Kind: "update_models", ResourceID: workspaceID, Stage: "admission", RequestID: c.GetRequestId(), AcceptedInput: accepted})
	if err != nil {
		return ownerstore.Operation{}, 0, false, dbError(err)
	}
	idem.ResourceID = workspaceID
	idem.OperationID = opID
	idem.ResponseBody = wire(modelUpdateOperationFrom(op))
	if err = s.Store.RecordIdempotency(ctx, tx, idem); err != nil {
		return ownerstore.Operation{}, 0, false, dbError(err)
	}
	if err = tx.Commit(); err != nil {
		return ownerstore.Operation{}, 0, false, dbError(err)
	}
	return op, targetVersion, false, nil
}

// runModelUpdate executes the owner-separated sequence for one accepted update and
// reports the Operation actually established. The Workspace reads back the managed
// key binding it already holds, refuses a selection the binding is not approved
// for, records the new configuration under that same binding, and lets Serve
// perform the Secret handover and the apply. The Workspace's applied version
// advances only from Serve's confirmed readback.
func (s *Service) runModelUpdate(ctx context.Context, op ownerstore.Operation, launch modelLaunch, credential contracts.WorkspaceApplicationCredential, selections []*api.ModelSelection, appliedVersion, targetVersion int64) (*api.Operation, error) {
	modelIDs := make([]string, 0, len(selections))
	for _, selection := range selections {
		modelIDs = append(modelIDs, selection.GetModelId())
	}
	// The key this Workspace already holds is the one the update must preserve. A
	// prior configuration that was definitely refused changed no key fact, so the
	// same binding remains usable; an unresolved prior configuration still blocks
	// new side effects until its owner fact is readable.
	keyBindingID, err := s.activeManagedKeyBindingID(ctx, op.ResourceID, launch.keyBindingID)
	if err != nil {
		return s.failModelUpdate(ctx, op, "configuration", err)
	}
	if strings.TrimSpace(keyBindingID) == "" {
		return s.failModelUpdate(ctx, op, "configuration", status.Errorf(codes.FailedPrecondition, "%s: the Workspace has no Gateway managed key binding to preserve", ReasonModelConfigurationUnavailable))
	}
	// 1. The Gateway owner reads back the exact binding and the model scope it is
	// approved for. The Workspace never mints a replacement for an ordinary model
	// update, so the external key identity stays the same across configurations.
	if err = s.advanceModelUpdate(ctx, op, "key"); err != nil {
		return nil, err
	}
	binding, err := s.Gateway.ReadManagedKey(ctx, &api.ManagedKeyReadCommand{Context: continuation(op, launch.grantID, "read_managed_key"), KeyBindingId: keyBindingID, WorkspaceId: op.ResourceID})
	if err != nil {
		return s.failModelUpdate(ctx, op, "key", err)
	}
	if binding.GetKeyBindingId() != keyBindingID || binding.GetWorkspaceId() != op.ResourceID ||
		strings.TrimSpace(binding.GetFingerprint()) == "" || strings.TrimSpace(binding.GetSecretDeliveryReference()) == "" ||
		binding.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef(op.ResourceID) {
		return s.failModelUpdate(ctx, op, "key", status.Error(codes.DataLoss, "Gateway returned a managed key binding that differs from the accepted update"))
	}
	// 2. The selection must already be inside the key's approved scope. Widening a
	// live key is a separate owner-authorized scope change that the external key
	// authority has not exposed yet, so an expansion is refused before the intent is
	// recorded and never applied under a wider local claim.
	if missing := managedKeyScopeMissing(binding.GetModelIds(), modelIDs); len(missing) > 0 {
		return s.failModelUpdate(ctx, op, "key", owneridentity.WithErrorCode(status.Errorf(codes.FailedPrecondition, "%s: managed key %s is not approved for %s", ReasonManagedKeyScopeExpansionRequired, keyBindingID, strings.Join(missing, ",")), api.ErrorCodeEnum_ERROR_CODE_ENUM_MODEL_NOT_ALLOWED))
	}
	// 3. The returned binding identity is recorded with the new configuration
	// version before Serve is asked to apply it, so the configuration's foreign key
	// and the Gateway fact are one durable intent naming the same key.
	configurationID := id("configuration_")
	if err = s.recordModelConfigurationIntent(ctx, configurationID, op, binding, selections, targetVersion); err != nil {
		return nil, err
	}
	// 4. Serve performs the managed-key handover itself: the command carries only the
	// Gateway key binding, fingerprint, Secret delivery reference and publisher slot,
	// never a Fabric-confirmed binding. Serve compares the handover with the exact
	// predecessor it recorded, confirms the same Secret through Fabric, applies the
	// publisher contract and reports the version the application itself read back.
	if err = s.advanceModelUpdate(ctx, op, "reload"); err != nil {
		return nil, err
	}
	reload := &api.RuntimeReloadCommand{Context: continuation(op, launch.grantID, "reload_models"), RuntimeInstanceId: launch.runtimeInstanceID, ExpectedAppliedVersion: appliedVersion, TargetVersion: targetVersion, Selections: selections, ManagedKeyBinding: &api.RuntimeManagedKeyBinding{KeyBindingId: binding.GetKeyBindingId(), SecretDeliveryReference: binding.GetSecretDeliveryReference(), Fingerprint: binding.GetFingerprint(), TargetSlot: credential.Name}}
	reloaded, err := s.Serve.ReloadModels(ctx, reload)
	if err != nil {
		return s.rejectModelConfiguration(ctx, op, configurationID, "reload", err)
	}
	if reloaded.GetOperationId() == "" || reloaded.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE || reloaded.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		return s.rejectModelConfiguration(ctx, op, configurationID, "reload", status.Errorf(codes.FailedPrecondition, "%s: Serve did not confirm the applied model configuration", ReasonModelConfigurationUnavailable))
	}
	// 5. Only now does the Workspace advance its applied version, in the same
	// transaction that records the confirmed reload. The key binding is unchanged,
	// so there is no predecessor to retire and no missing key fact on a lost commit.
	if err = s.confirmModelConfiguration(ctx, op, configurationID, binding.GetKeyBindingId(), appliedVersion, targetVersion); err != nil {
		_, code := modelUpdateFailure(err)
		return s.finishModelUpdate(ctx, op, "verification", "needs_attention", "unknown", "", code, orderResult{ConfigurationID: configurationID, ConfigurationVersion: targetVersion, KeyBindingID: binding.GetKeyBindingId(), ServeOperationID: reloaded.GetOperationId()})
	}
	return s.finishModelUpdate(ctx, op, "succeeded", "succeeded", "confirmed", "", "", orderResult{ConfigurationID: configurationID, ConfigurationVersion: targetVersion, KeyBindingID: binding.GetKeyBindingId(), ServeOperationID: reloaded.GetOperationId()})
}

// activeManagedKeyBindingID resolves the Gateway managed key binding this
// Workspace's model configuration must keep using. A completed confirmed update
// names the binding it recorded; while no configuration exists the launch binding is
// the one to preserve. A recorded configuration that is not confirmed may already
// have changed the runtime, so it blocks new side effects until its owner fact is
// readable rather than letting the update guess which key is live.
func (s *Service) activeManagedKeyBindingID(ctx context.Context, workspaceID, launchBindingID string) (string, error) {
	var raw []byte
	var observation, operationState, recorded string
	err := s.Store.DB().QueryRowContext(ctx, `SELECT c.runtime_reload_observation,COALESCE(o.result,'{}'::jsonb),o.status,c.gateway_key_binding_id
		FROM workspace.model_configurations c
		JOIN workspace.operations o ON o.id=c.operation_id
		WHERE c.workspace_id=$1 ORDER BY c.version DESC LIMIT 1`, workspaceID).Scan(&observation, &raw, &operationState, &recorded)
	if errors.Is(err, sql.ErrNoRows) {
		return strings.TrimSpace(launchBindingID), nil
	}
	if err != nil {
		return "", dbError(err)
	}
	recorded = strings.TrimSpace(recorded)
	if observation != "confirmed" || operationState != "succeeded" {
		return "", status.Error(codes.Unavailable, "the previous model configuration closeout is unresolved")
	}
	if recorded == "" {
		return "", status.Error(codes.DataLoss, "confirmed model configuration has no recorded Gateway key binding")
	}
	return recorded, nil
}

// managedKeyScopeMissing reports the requested model ids the approved managed key
// scope does not cover. An empty result means the update stays inside the key's
// existing allowlist; anything else requires an owner-authorized scope change the
// Workspace must not perform by minting a wider key.
func managedKeyScopeMissing(approved, requested []string) []string {
	// An empty projection means the external Gateway Group is the authoritative
	// routing and model scope. Cloud has no second allowlist to compare here.
	if len(approved) == 0 {
		return nil
	}
	covered := make(map[string]bool, len(approved))
	for _, model := range approved {
		if model = strings.TrimSpace(model); model != "" {
			covered[model] = true
		}
	}
	missing := make([]string, 0)
	seen := map[string]bool{}
	for _, model := range requested {
		model = strings.TrimSpace(model)
		if model == "" || covered[model] || seen[model] {
			continue
		}
		seen[model] = true
		missing = append(missing, model)
	}
	return missing
}

// recordModelConfigurationIntent writes the accepted configuration version and the
// Gateway binding it belongs to. The row references the update operation, so the
// configuration and the operation that carries it are one fact.
func (s *Service) recordModelConfigurationIntent(ctx context.Context, configurationID string, op ownerstore.Operation, binding *api.ManagedKeyBinding, selections []*api.ModelSelection, version int64) error {
	raw, err := modelSelectionsJSON(selections)
	if err != nil {
		return err
	}
	if _, err = s.Store.DB().ExecContext(ctx, `INSERT INTO workspace.model_configurations(id,workspace_id,version,gateway_key_binding_id,operation_id,runtime_reload_observation,created_by,selections)
		VALUES($1,$2,$3,$4,$5,'unknown',$6,$7)`, configurationID, op.ResourceID, version, binding.GetKeyBindingId(), op.ID, op.ActorID, raw); err != nil {
		return dbError(err)
	}
	return nil
}

// confirmModelConfiguration records the confirmed reload and advances the
// Workspace's applied model configuration version in one transaction. The
// compare-and-set requires the version the caller expected, so a configuration
// that the application did not confirm can never be recorded as applied. The
// evidence names the Gateway key binding the configuration was applied under,
// because the Workspace no longer observes a per-configuration Fabric version.
func (s *Service) confirmModelConfiguration(ctx context.Context, op ownerstore.Operation, configurationID, keyBindingID string, expectedAppliedVersion, targetVersion int64) error {
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return dbError(err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE workspace.model_configurations SET runtime_reload_observation='confirmed',verification_evidence_ref=$2,updated_at=now() WHERE id=$1 AND runtime_reload_observation<>'confirmed'`, configurationID, "workspace-model-configuration://"+configurationID+"/"+keyBindingID)
	if err != nil {
		return dbError(err)
	}
	if affected, err := res.RowsAffected(); err != nil || affected != 1 {
		return owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "the model configuration is no longer awaiting its confirmation"), api.ErrorCodeEnum_ERROR_CODE_ENUM_VERSION_CONFLICT)
	}
	res, err = tx.ExecContext(ctx, `UPDATE workspace.workspaces SET model_configuration_version=$2,version=version+1,updated_at=now() WHERE id=$1 AND model_configuration_version=$3`, op.ResourceID, targetVersion, expectedAppliedVersion)
	if err != nil {
		return dbError(err)
	}
	if affected, err := res.RowsAffected(); err != nil || affected != 1 {
		return owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "the Workspace applied model configuration changed during the update"), api.ErrorCodeEnum_ERROR_CODE_ENUM_VERSION_CONFLICT)
	}
	if err = tx.Commit(); err != nil {
		return dbError(err)
	}
	return nil
}

// advanceModelUpdate records the stage the update reached. The stage vocabulary is
// the contract's own update_models sequence, so the Operation reports exactly where
// an accepted update stands.
func (s *Service) advanceModelUpdate(ctx context.Context, op ownerstore.Operation, stage string) error {
	if _, err := s.Store.DB().ExecContext(ctx, `UPDATE workspace.operations SET status='running',stage=$2,observation_result='unknown',started_at=COALESCE(started_at,now()),updated_at=now() WHERE id=$1 AND status NOT IN ('succeeded','failed','cancelled')`, op.ID, stage); err != nil {
		return dbError(err)
	}
	return nil
}

// failModelUpdate records an update that failed before a configuration depended on
// it, and reports the exact cause. The Workspace applied version is untouched.
func (s *Service) failModelUpdate(ctx context.Context, op ownerstore.Operation, stage string, cause error) (*api.Operation, error) {
	observation, code := modelUpdateFailure(cause)
	return s.finishModelUpdate(ctx, op, stage, "needs_attention", observation, "", code, orderResult{})
}

// rejectModelConfiguration records an update whose configuration version was
// written but not confirmed, and reports the exact reason. The applied version
// never advances from an unconfirmed reload.
func (s *Service) rejectModelConfiguration(ctx context.Context, op ownerstore.Operation, configurationID, stage string, cause error) (*api.Operation, error) {
	observation, code := modelUpdateFailure(cause)
	if _, err := s.Store.DB().ExecContext(ctx, `UPDATE workspace.model_configurations SET runtime_reload_observation=$2,updated_at=now() WHERE id=$1`, configurationID, observation); err != nil {
		return nil, dbError(err)
	}
	return s.finishModelUpdate(ctx, op, stage, "needs_attention", observation, "", code, orderResult{ConfigurationID: configurationID})
}

// modelUpdateFailure classifies one failed step. A refusal an owner answered is
// rejected; a transport or availability failure leaves the outcome unknown, so an
// unanswered reload is never recorded as a refusal.
func modelUpdateFailure(cause error) (observation, code string) {
	observation = "unknown"
	switch status.Code(cause) {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.AlreadyExists, codes.NotFound, codes.PermissionDenied, codes.Unauthenticated, codes.ResourceExhausted, codes.OutOfRange:
		observation = "rejected"
	}
	// An owner's explicit unknown outcome is stronger evidence than the transport
	// category. Integrity failures likewise cannot prove that an effect was refused.
	if ownerCode, found := owneridentity.ErrorCode(cause); found {
		if !validErrorCode(ownerCode) {
			return "unknown", errorCodeText(api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR)
		}
		// These reasons are synchronous-only in the current API contract. Once
		// Workspace accepted the operation, an inadmissible dependency reason
		// is its internal call/translation failure, not a new business conflict.
		switch ownerCode {
		case api.ErrorCodeEnum_ERROR_CODE_ENUM_UNAUTHENTICATED,
			api.ErrorCodeEnum_ERROR_CODE_ENUM_CSRF_INVALID,
			api.ErrorCodeEnum_ERROR_CODE_ENUM_ORIGIN_REJECTED,
			api.ErrorCodeEnum_ERROR_CODE_ENUM_IDEMPOTENCY_REQUIRED,
			api.ErrorCodeEnum_ERROR_CODE_ENUM_IDEMPOTENCY_CONFLICT:
			ownerCode = api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR
		}
		if ownerCode == api.ErrorCodeEnum_ERROR_CODE_ENUM_EXTERNAL_OUTCOME_UNKNOWN {
			observation = "unknown"
		}
		return observation, errorCodeText(ownerCode)
	}
	// Same-domain metadata with an undefined reason is an invalid owner answer,
	// not an untyped refusal. Do not guess a business outcome from its transport.
	for _, detail := range status.Convert(cause).Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok && info.GetDomain() == owneridentity.ErrorDomain {
			return "unknown", errorCodeText(api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR)
		}
	}
	var ownerCode api.ErrorCodeEnum
	switch status.Code(cause) {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.OutOfRange:
		ownerCode = api.ErrorCodeEnum_ERROR_CODE_ENUM_VALIDATION_FAILED
	case codes.NotFound:
		ownerCode = api.ErrorCodeEnum_ERROR_CODE_ENUM_NOT_FOUND
	case codes.PermissionDenied:
		ownerCode = api.ErrorCodeEnum_ERROR_CODE_ENUM_FORBIDDEN
	case codes.Unauthenticated:
		ownerCode = api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR
	case codes.ResourceExhausted:
		ownerCode = api.ErrorCodeEnum_ERROR_CODE_ENUM_RATE_LIMITED
	case codes.Unavailable, codes.DeadlineExceeded:
		ownerCode = api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE
	case codes.Unimplemented:
		ownerCode = api.ErrorCodeEnum_ERROR_CODE_ENUM_OWNER_CAPABILITY_UNAVAILABLE
	case codes.Canceled, codes.Unknown:
		ownerCode = api.ErrorCodeEnum_ERROR_CODE_ENUM_EXTERNAL_OUTCOME_UNKNOWN
	default:
		// DataLoss/Internal cannot confirm an outcome. Untyped conflicts do not
		// identify VERSION_CONFLICT or IDEMPOTENCY_CONFLICT. Record Workspace's
		// translation failure without dropping the accepted operation's observation.
		ownerCode = api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR
	}
	return observation, errorCodeText(ownerCode)
}

// finishModelUpdate records the terminal state of one update attempt together with
// the identities it established, and returns the owner row exactly as committed.
func (s *Service) finishModelUpdate(ctx context.Context, op ownerstore.Operation, stage, state, observation, ownerRef, code string, result orderResult) (*api.Operation, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, status.Error(codes.Internal, "Workspace model update cannot be encoded")
	}
	if _, err = s.Store.DB().ExecContext(ctx, `UPDATE workspace.operations SET result=$2,status=$3,stage=$4,observation_result=$5,error_code=NULLIF($6,''),updated_at=now(),completed_at=CASE WHEN $3 IN ('succeeded','failed','cancelled') THEN now() ELSE completed_at END WHERE id=$1`, op.ID, raw, state, stage, observation, code); err != nil {
		return nil, dbError(err)
	}
	updated, err := s.Store.ReadOperation(ctx, op.ID)
	if err != nil {
		return nil, dbError(err)
	}
	return modelUpdateOperation(updated)
}

// modelUpdateOperation projects one owner-local update_models row as the typed
// Operation the caller polls. Kind, stage, status and observation are resolved
// through the contract's own vocabulary, so an unknown stored value is an error
// instead of a silent zero enum.
func modelUpdateOperation(op ownerstore.Operation) (*api.Operation, error) {
	out := &api.Operation{OperationId: op.ID, Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE, ResourceId: op.ResourceID, RequestId: op.RequestID, CreatedAt: timestamppb.New(op.CreatedAt.UTC()), UpdatedAt: timestamppb.New(op.UpdatedAt.UTC())}
	kind, ok := api.OperationKindEnum_value["OPERATION_KIND_ENUM_"+strings.ToUpper(op.Kind)]
	if !ok {
		return nil, status.Error(codes.Internal, "stored Workspace operation kind is invalid")
	}
	out.Kind = api.OperationKindEnum(kind)
	stage, ok := api.OperationStageEnum_value["OPERATION_STAGE_ENUM_"+strings.ToUpper(op.Stage)]
	if !ok {
		return nil, status.Error(codes.Internal, "stored Workspace operation stage is invalid")
	}
	out.Stage = api.OperationStageEnum(stage)
	state, ok := api.OperationStatusEnum_value["OPERATION_STATUS_ENUM_"+strings.ToUpper(op.Status)]
	if !ok {
		return nil, status.Error(codes.Internal, "stored Workspace operation status is invalid")
	}
	out.Status = api.OperationStatusEnum(state)
	if op.Observation != "" {
		observation, ok := api.OperationObservationResultEnum_value["OPERATION_OBSERVATION_RESULT_ENUM_"+strings.ToUpper(op.Observation)]
		if !ok {
			return nil, status.Error(codes.Internal, "stored Workspace operation observation is invalid")
		}
		value := api.OperationObservationResultEnum(observation)
		out.ObservationResult = &value
	}
	if op.ErrorCode != "" {
		code, ok := api.ErrorCodeEnum_value["ERROR_CODE_ENUM_"+strings.ToUpper(strings.TrimSpace(op.ErrorCode))]
		if !ok {
			return nil, status.Error(codes.Internal, "stored Workspace operation error code is invalid")
		}
		value := api.ErrorCodeEnum(code)
		out.ErrorCode = &value
	}
	if !op.Terminal() {
		out.PollAfterSeconds = proto.Int32(5)
	}
	return out, nil
}

// modelUpdateOperationFrom projects a freshly created update row without a second
// read, so the idempotency record and the response describe the same operation.
func modelUpdateOperationFrom(op ownerstore.Operation) *api.Operation {
	out, _ := modelUpdateOperation(op)
	return out
}
