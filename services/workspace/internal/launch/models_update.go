package launch

// This file owns the Workspace's model-configuration update: the one
// owner-separated operation that turns an accepted model intent into a
// confirmed, applied configuration. The owners stay separate on purpose -
// Workspace authorizes and versions the intent, Gateway issues and allowlists the
// managed key, Fabric confirms the runtime Secret binding, and Serve applies the
// publisher contract and reports the version the application itself read back.
// The applied column therefore advances only from Serve's confirmed readback, and
// the public request never carries a Gateway key binding.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// UpdateWorkspaceModels applies one accepted model configuration to the
// Workspace's current runtime. A new configuration version is created for the
// new selections, the Gateway-managed key for exactly that model set is created
// and bound into the runtime through Fabric, and Serve applies and reads back the
// publisher configuration. Only a confirmed readback advances the Workspace's own
// applied version, so an unanswered or refused reload never presents the
// requested configuration as the applied one.
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
	if s.Gateway == nil || s.Fabric == nil || s.Serve == nil {
		return nil, status.Error(codes.Unavailable, "Gateway, Fabric and Serve are required to update a model configuration")
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
	// The frozen Runtime Release must also declare the publisher interface a reload
	// executes. Both facts are required before any owner effect: a release that
	// declares no interface is refused here rather than after a Gateway key was
	// minted and a runtime Secret was bound.
	if err = s.requireDeclaredModelConfiguration(ctx, launch); err != nil {
		return nil, err
	}
	op, targetVersion, replay, err := s.acceptModelUpdate(ctx, c, workspaceID, body, tenant)
	if err != nil {
		return nil, err
	}
	if replay {
		return modelUpdateOperation(op)
	}
	return s.runModelUpdate(ctx, op, launch, credential, selections, appliedVersion, targetVersion)
}

// ReasonModelConfigurationUnavailable names a runtime whose frozen revision
// declares no installation Gateway credential, so no model configuration can be
// applied to it.
const ReasonModelConfigurationUnavailable = "model_configuration_unavailable"

// ReasonModelConfigurationSourceUnresolved names the launch fact a model update
// needs and the frozen application source cannot prove: the exact approved Runtime
// Release the publisher interface belongs to. A launch that names neither a release
// nor an admitted Agent naming one is refused instead of guessed at.
const ReasonModelConfigurationSourceUnresolved = "model_configuration_source_unresolved"

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

// requireDeclaredModelConfiguration proves the frozen Runtime Release of this launch
// declares the publisher model-configuration interface before the update performs any
// owner effect. The interface is a fact of the approved Runtime Release the launch
// froze: the default App names it directly, and a built Agent names the release its
// admitted CapabilityVersion was built against. Serve resolves the same declaration
// again when it reloads, so an undeclared interface is refused by both owners and
// never applied.
func (s *Service) requireDeclaredModelConfiguration(ctx context.Context, launch modelLaunch) error {
	resolved, err := launch.source.resolve()
	if err != nil {
		return err
	}
	call := continuation(launch.operation, launch.grantID, "model_capability")
	releaseID := resolved.RuntimeVersionID
	if releaseID == "" {
		if resolved.Selection.GetKind() != api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_AGENT || resolved.CapabilityVersionID == "" {
			return status.Errorf(codes.FailedPrecondition, "%s: the launch names no approved Runtime Release", ReasonModelConfigurationSourceUnresolved)
		}
		if s.Capability == nil {
			return status.Error(codes.Unavailable, "Capability is not configured")
		}
		version, err := s.Capability.GetCapabilityVersion(ctx, &api.GetCapabilityVersionRpcRequest{Context: call, CapabilityVersionId: resolved.CapabilityVersionID})
		if err != nil {
			return err
		}
		if version.GetId() != resolved.CapabilityVersionID || version.GetArtifact().GetDigest() != resolved.Artifact.GetDigest() {
			return status.Error(codes.FailedPrecondition, "Capability did not confirm the exact deployed version")
		}
		releaseID = strings.TrimSpace(version.GetRuntimeVersionId())
		if releaseID == "" {
			return status.Errorf(codes.FailedPrecondition, "%s: the admitted Agent names no approved Runtime Release", ReasonModelConfigurationSourceUnresolved)
		}
	}
	release, err := s.approvedRuntimeRelease(ctx, call, releaseID)
	if err != nil {
		return err
	}
	if release.GetPublisherContract().GetModelConfiguration() == nil {
		return status.Errorf(codes.FailedPrecondition, "%s: the frozen Runtime Release declares no model-configuration interface", ReasonModelConfigurationUnavailable)
	}
	return nil
}

// modelLaunch is the frozen launch fact the update path continues: the bounded
// grant the accepted order obtained, the runtime instance that order delivered and
// the immutable application source it was admitted against.
type modelLaunch struct {
	// operation is the accepted create_workspace operation this update continues. Its
	// identity and the bounded grant below are the continuation every owner read and
	// effect the update issues is admitted under.
	operation         ownerstore.Operation
	grantID           string
	runtimeInstanceID string
	source            *sourceRecord
	// keyBindingID is the launch-issued Gateway key binding for a runtime whose
	// revision declares the installation Gateway credential. The first model
	// configuration replaces it, so it is the predecessor key the update revokes
	// once Serve confirms the new version.
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
	var operationID string
	var raw []byte
	err := s.Store.DB().QueryRowContext(ctx, `SELECT id,COALESCE(result,'{}'::jsonb) FROM workspace.operations
		WHERE resource_id=$1 AND kind='create_workspace' ORDER BY created_at DESC, id DESC LIMIT 1`, workspaceID).Scan(&operationID, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return modelLaunch{}, status.Error(codes.FailedPrecondition, "the Workspace has no accepted launch")
	}
	if err != nil {
		return modelLaunch{}, dbError(err)
	}
	operation, err := s.Store.ReadOperation(ctx, operationID)
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
	return modelLaunch{operation: operation, grantID: result.GrantID, runtimeInstanceID: command.GetRuntimeInstanceId(), source: source, keyBindingID: keyBindingID}, nil
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
// reports the Operation actually established. Every step is recorded against the
// update operation, and the Workspace's applied version advances only from Serve's
// confirmed readback.
func (s *Service) runModelUpdate(ctx context.Context, op ownerstore.Operation, launch modelLaunch, credential contracts.WorkspaceApplicationCredential, selections []*api.ModelSelection, appliedVersion, targetVersion int64) (*api.Operation, error) {
	modelIDs := make([]string, 0, len(selections))
	for _, selection := range selections {
		modelIDs = append(modelIDs, selection.GetModelId())
	}
	// 1-2. Gateway issues and allowlists the managed key for exactly this model set
	// and this runtime; the raw key never reaches Workspace.
	if err := s.advanceModelUpdate(ctx, op, "key"); err != nil {
		return nil, err
	}
	binding, err := s.Gateway.CreateManagedKey(ctx, &api.ManagedKeyCommand{Context: continuation(op, launch.grantID, "create_managed_key"), WorkspaceId: op.ResourceID, ModelIds: modelIDs, TargetRuntimeInstanceId: launch.runtimeInstanceID})
	if err != nil {
		return s.failModelUpdate(ctx, op, "key", err)
	}
	if strings.TrimSpace(binding.GetKeyBindingId()) == "" || strings.TrimSpace(binding.GetSecretDeliveryReference()) == "" || strings.TrimSpace(binding.GetFingerprint()) == "" ||
		binding.GetWorkspaceId() != op.ResourceID || binding.GetTargetRuntimeInstanceId() != launch.runtimeInstanceID || binding.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef(op.ResourceID) {
		return s.failModelUpdate(ctx, op, "key", status.Error(codes.DataLoss, "Gateway returned a managed key that differs from the accepted update"))
	}
	// 3. The returned binding identity is recorded with the new configuration
	// version before the Secret is bound, so the configuration's foreign key and the
	// Gateway fact are one durable intent.
	configurationID := id("configuration_")
	if err = s.recordModelConfigurationIntent(ctx, configurationID, op, binding, selections, targetVersion); err != nil {
		return nil, err
	}
	// 4. Fabric binds the approved Secret into the exact runtime and confirms the
	// version the execution boundary must inject. An unconfirmed binding never
	// reaches Serve. A runtime that already carries a confirmed Fabric binding for
	// this purpose is a replacement: the first configuration binds initially, and a
	// later configuration rebinds against the exact predecessor it recorded. Fabric
	// never silently replaces a different Secret, so the two operations stay
	// distinct and the predecessor stays active until the replacement is confirmed.
	if err = s.advanceModelUpdate(ctx, op, "configuration"); err != nil {
		return nil, err
	}
	predecessor, err := s.activeModelSecretBindingID(ctx, op.ResourceID)
	if err != nil {
		return nil, err
	}
	secretBindingID, secretVersion, err := s.bindOrRebindModelSecret(ctx, op, launch, credential.Name, predecessor, binding)
	if err != nil {
		return s.rejectModelConfiguration(ctx, op, configurationID, "configuration", err)
	}
	// 5. Serve applies the publisher contract and reports the version the
	// application itself read back. The opaque binding travels with the command and
	// Serve issues no Gateway key of its own.
	if err = s.advanceModelUpdate(ctx, op, "reload"); err != nil {
		return nil, err
	}
	reload := &api.RuntimeReloadCommand{Context: continuation(op, launch.grantID, "reload_models"), RuntimeInstanceId: launch.runtimeInstanceID, ExpectedAppliedVersion: appliedVersion, TargetVersion: targetVersion, Selections: selections, ManagedKeyBinding: &api.RuntimeManagedKeyBinding{KeyBindingId: binding.GetKeyBindingId(), SecretDeliveryReference: binding.GetSecretDeliveryReference(), Fingerprint: binding.GetFingerprint(), TargetSlot: credential.Name, SecretBindingId: secretBindingID, SecretVersion: secretVersion}}
	reloaded, err := s.Serve.ReloadModels(ctx, reload)
	if err != nil {
		return s.rejectModelConfiguration(ctx, op, configurationID, "reload", err)
	}
	if reloaded.GetOperationId() == "" || reloaded.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE || reloaded.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		return s.rejectModelConfiguration(ctx, op, configurationID, "reload", status.Errorf(codes.FailedPrecondition, "%s: Serve did not confirm the applied model configuration", ReasonModelConfigurationUnavailable))
	}
	// 6. Only now does the Workspace advance its applied version, in the same
	// transaction that records the confirmed reload.
	if err = s.confirmModelConfiguration(ctx, op, configurationID, secretVersion, targetVersion); err != nil {
		return nil, err
	}
	// The superseded key is retired after the new one is confirmed, so a
	// configuration change never leaves the previous generation live.
	revoked, revokeErr := s.revokeSupersededKey(ctx, op, launch, targetVersion)
	if revokeErr != nil {
		return s.finishModelUpdate(ctx, op, "verification", "needs_attention", "unknown", "", status.Code(revokeErr).String(), orderResult{ConfigurationID: configurationID, ConfigurationVersion: targetVersion, KeyBindingID: binding.GetKeyBindingId(), SecretBindingID: secretBindingID, ServeOperationID: reloaded.GetOperationId(), RevokedKeyBindingID: revoked})
	}
	return s.finishModelUpdate(ctx, op, "succeeded", "succeeded", "confirmed", "", "", orderResult{ConfigurationID: configurationID, ConfigurationVersion: targetVersion, KeyBindingID: binding.GetKeyBindingId(), SecretBindingID: secretBindingID, ServeOperationID: reloaded.GetOperationId(), RevokedKeyBindingID: revoked})
}

// activeModelSecretBindingID returns the Fabric Secret binding this Workspace's
// last confirmed model configuration recorded for its runtime. It is the exact
// predecessor a later configuration must name when it rebinds: the first
// configuration binds initially, so an absent value means there is nothing to
// replace. The binding id is the opaque owner readback Workspace stored with the
// configuration it confirmed, not a value the caller can choose.
func (s *Service) activeModelSecretBindingID(ctx context.Context, workspaceID string) (string, error) {
	var raw []byte
	err := s.Store.DB().QueryRowContext(ctx, `SELECT COALESCE(result,'{}'::jsonb) FROM workspace.operations
		WHERE resource_id=$1 AND kind='update_models' AND status='succeeded'
		ORDER BY created_at DESC, id DESC LIMIT 1`, workspaceID).Scan(&raw)
	if err == nil {
		var result orderResult
		if json.Unmarshal(raw, &result) != nil {
			return "", status.Error(codes.DataLoss, "stored Workspace model update result is invalid")
		}
		// A prior confirmed configuration already bound the runtime Secret, so its
		// recorded binding is the exact predecessor to replace.
		return strings.TrimSpace(result.SecretBindingID), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", dbError(err)
	}
	// No configuration update has ever succeeded, so the active binding is the one
	// the launch established for the runtime's declared Gateway credential. It is the
	// predecessor the first model configuration replaces; a launch that bound nothing
	// (no declared credential) leaves this empty, so the first configuration binds
	// initially instead of rebinding.
	var launchRaw []byte
	err = s.Store.DB().QueryRowContext(ctx, `SELECT COALESCE(result,'{}'::jsonb) FROM workspace.operations
		WHERE resource_id=$1 AND kind='create_workspace'
		ORDER BY created_at DESC, id DESC LIMIT 1`, workspaceID).Scan(&launchRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", dbError(err)
	}
	var launch orderResult
	if json.Unmarshal(launchRaw, &launch) != nil {
		return "", status.Error(codes.DataLoss, "stored Workspace launch result is invalid")
	}
	if len(launch.ManagedKeyBinding) == 0 {
		return "", nil
	}
	binding := &api.RuntimeManagedKeyBinding{}
	if protojson.Unmarshal(launch.ManagedKeyBinding, binding) != nil {
		return "", status.Error(codes.DataLoss, "stored launch managed key binding is invalid")
	}
	return strings.TrimSpace(binding.GetSecretBindingId()), nil
}

// bindOrRebindModelSecret confirms the approved Secret for one model configuration.
// With no predecessor the first configuration binds initially; with a predecessor
// the replacement must name that exact Fabric binding so Fabric compares the active
// binding before it supersedes. Both paths return the confirmed binding identity and
// version Serve must inject; a replacement reports the predecessor it retired so the
// update receipt can record it.
func (s *Service) bindOrRebindModelSecret(ctx context.Context, op ownerstore.Operation, launch modelLaunch, targetSlot, predecessor string, binding *api.ManagedKeyBinding) (string, string, error) {
	if strings.TrimSpace(predecessor) == "" {
		bound, err := s.Fabric.BindSecret(ctx, &api.SecretBindingCommand{Context: continuation(op, launch.grantID, "bind_managed_secret"), WorkspaceId: op.ResourceID, RuntimeInstanceId: launch.runtimeInstanceID, KeyBindingId: binding.GetKeyBindingId(), SecretDeliveryReference: binding.GetSecretDeliveryReference(), TargetSlot: targetSlot, Fingerprint: binding.GetFingerprint()})
		if err != nil {
			return "", "", err
		}
		if bound.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || strings.TrimSpace(bound.GetSecretBindingId()) == "" || strings.TrimSpace(bound.GetVersion()) == "" ||
			bound.GetFingerprint() != binding.GetFingerprint() || bound.GetRuntimeInstanceId() != launch.runtimeInstanceID {
			return "", "", status.Error(codes.FailedPrecondition, "Fabric did not confirm the exact Gateway Secret binding")
		}
		return bound.GetSecretBindingId(), bound.GetVersion(), nil
	}
	rebound, err := s.Fabric.RebindSecret(ctx, &api.SecretBindingRebindCommand{Context: continuation(op, launch.grantID, "rebind_managed_secret"), WorkspaceId: op.ResourceID, RuntimeInstanceId: launch.runtimeInstanceID, ExpectedCurrentSecretBindingId: predecessor, KeyBindingId: binding.GetKeyBindingId(), SecretDeliveryReference: binding.GetSecretDeliveryReference(), TargetSlot: targetSlot, Fingerprint: binding.GetFingerprint()})
	if err != nil {
		return "", "", err
	}
	if rebound.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || strings.TrimSpace(rebound.GetSecretBindingId()) == "" || strings.TrimSpace(rebound.GetVersion()) == "" ||
		rebound.GetFingerprint() != binding.GetFingerprint() || rebound.GetRuntimeInstanceId() != launch.runtimeInstanceID ||
		strings.TrimSpace(rebound.GetPreviousSecretBindingId()) != predecessor {
		return "", "", status.Error(codes.FailedPrecondition, "Fabric did not confirm the exact Gateway Secret replacement")
	}
	return rebound.GetSecretBindingId(), rebound.GetVersion(), nil
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
// that the application did not confirm can never be recorded as applied.
func (s *Service) confirmModelConfiguration(ctx context.Context, op ownerstore.Operation, configurationID, secretVersion string, targetVersion int64) error {
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return dbError(err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE workspace.model_configurations SET runtime_reload_observation='confirmed',verification_evidence_ref=$2,updated_at=now() WHERE id=$1 AND runtime_reload_observation<>'confirmed'`, configurationID, "workspace-model-configuration://"+configurationID+"/"+secretVersion)
	if err != nil {
		return dbError(err)
	}
	if affected, err := res.RowsAffected(); err != nil || affected != 1 {
		return status.Error(codes.FailedPrecondition, "the model configuration is no longer awaiting its confirmation")
	}
	res, err = tx.ExecContext(ctx, `UPDATE workspace.workspaces SET model_configuration_version=$2,version=version+1,updated_at=now() WHERE id=$1 AND model_configuration_version<>$2`, op.ResourceID, targetVersion)
	if err != nil {
		return dbError(err)
	}
	if affected, err := res.RowsAffected(); err != nil || affected != 1 {
		return status.Error(codes.FailedPrecondition, "the Workspace applied model configuration changed during the update")
	}
	if err = tx.Commit(); err != nil {
		return dbError(err)
	}
	return nil
}

// revokeSupersededKey retires the Gateway key binding the new configuration
// replaces. It runs only after the new binding is confirmed, is idempotent at
// Gateway, and reports the binding it retired so the update receipt names it.
func (s *Service) revokeSupersededKey(ctx context.Context, op ownerstore.Operation, launch modelLaunch, targetVersion int64) (string, error) {
	var previous string
	err := s.Store.DB().QueryRowContext(ctx, `SELECT gateway_key_binding_id FROM workspace.model_configurations
		WHERE workspace_id=$1 AND version<$2 ORDER BY version DESC LIMIT 1`, op.ResourceID, targetVersion).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		// No prior configuration row exists, so the launch-issued Gateway key is the
		// predecessor this first configuration replaces.
		previous = launch.keyBindingID
	} else if err != nil {
		return "", dbError(err)
	}
	if strings.TrimSpace(previous) == "" {
		return "", nil
	}
	if _, err = s.Gateway.RevokeManagedKey(ctx, &api.ManagedKeyRevoke{Context: continuation(op, launch.grantID, "revoke_superseded_key"), KeyBindingId: previous, WorkspaceId: op.ResourceID}); err != nil {
		return previous, err
	}
	return previous, nil
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
	code = status.Code(cause).String()
	switch status.Code(cause) {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.AlreadyExists, codes.NotFound, codes.PermissionDenied, codes.Unauthenticated, codes.ResourceExhausted, codes.OutOfRange, codes.DataLoss:
		return "rejected", code
	default:
		return "unknown", code
	}
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
