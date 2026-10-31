package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerstore"
)

func replacementSelection(body *api.UpdateWorkspaceVersionRequest) (*api.WorkspaceApplicationSelection, error) {
	if body == nil {
		return nil, status.Error(codes.InvalidArgument, "an update version body is required")
	}
	selection := body.GetApplicationSelection()
	if selection != nil {
		if body.CapabilityVersionId != nil {
			return nil, status.Error(codes.InvalidArgument, "application selection and capability version are mutually exclusive")
		}
	} else if body.CapabilityVersionId != nil {
		selection = &api.WorkspaceApplicationSelection{Kind: api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_AGENT, CapabilityVersionId: body.CapabilityVersionId}
	}
	if err := contracts.ValidateWorkspaceApplicationSelection(selection); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return selection, nil
}

func (s *Service) replacementSource(ctx context.Context, call *api.CallContext, selection *api.WorkspaceApplicationSelection) (replacementPlan, error) {
	var plan replacementPlan
	if contracts.IsDefaultOPLAppSelection(selection) {
		release, err := s.runtimeRelease(ctx, nextOwnerCall(call), selection.GetRuntimeVersionId())
		if err != nil {
			return plan, err
		}
		contract := release.GetPublisherContract()
		// This path executes compatible application replacements. Publisher migrations
		// and backups need their own actual execution evidence, never a boolean claim.
		if contract.GetData().GetUpgrade().GetMode() != api.DataUpgradeContractModeEnum_DATA_UPGRADE_CONTRACT_MODE_ENUM_COMPATIBLE || contract.GetData().GetUpgrade().GetBackupRequired() {
			return plan, status.Errorf(codes.FailedPrecondition, "%s: publisher migration or backup execution is required", ReasonDataCompatibilityUnproven)
		}
		descriptor := &api.DeploymentDescriptor{
			SchemaVersion: api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1,
			Artifact:      contract.GetImage(), Provenance: api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_RUNTIME_RELEASE,
			ApplicationSelection: selection, ApplicationRevision: contract.GetApplicationRevisionTemplate(), RuntimeContract: contract,
		}
		if _, err = deploymentRevision(&api.RuntimeDeployCommand{DeploymentDescriptor: descriptor}); err != nil {
			return plan, err
		}
		if descriptor.GetApplicationRevision().GetImage() != contract.GetImage().GetRepository()+"@"+contract.GetImage().GetDigest() || contract.GetData().GetSchemaVersion() == "" {
			return plan, status.Error(codes.FailedPrecondition, "Runtime Release has inconsistent image or missing data contract")
		}
		digest, err := descriptorDigest(descriptor)
		if err != nil {
			return plan, err
		}
		compatibility, err := runtimeReplacementCompatibility(contract)
		return replacementPlan{runtimeVersionID: release.GetId(), descriptor: descriptor, descriptorDigest: digest,
			descriptorRef: "runtime-release:" + release.GetId() + "@" + digest, compatibility: compatibility}, err
	}
	if s.Capability == nil {
		return plan, status.Error(codes.Unavailable, "Capability is not configured")
	}
	version, err := s.Capability.GetCapabilityVersion(ctx, &api.GetCapabilityVersionRpcRequest{Context: nextOwnerCall(call), CapabilityVersionId: selection.GetCapabilityVersionId()})
	if err != nil {
		return plan, err
	}
	if version.GetId() != selection.GetCapabilityVersionId() || version.GetStatus() != api.CapabilityVersionStatusEnum_CAPABILITY_VERSION_STATUS_ENUM_READY || version.GetDeploymentDescriptor() == nil {
		return plan, status.Errorf(codes.FailedPrecondition, "%s: Capability did not confirm the requested version as deployable", ReasonReplacementUnavailable)
	}
	compatibility, err := compatibilityJSON(version.GetDataCompatibility())
	return replacementPlan{capabilityVersionID: version.GetId(), descriptor: version.GetDeploymentDescriptor(), descriptorDigest: version.GetDeploymentDescriptorDigest(), descriptorRef: version.GetDeploymentDescriptorObjectRef(), compatibility: compatibility}, err
}

func runtimeReplacementCompatibility(contract *api.RuntimePublisherContract) ([]byte, error) {
	data := contract.GetData()
	if data.GetSchemaVersion() == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "%s: Runtime Release declares no data schema", ReasonDataCompatibilityUnproven)
	}
	return compatibilityJSON(&api.DataCompatibility{DataSchemaVersion: data.GetSchemaVersion(), CompatibleFromVersions: data.GetUpgrade().GetCompatibleFromSchemaVersions(), RollbackSafe: data.GetRollback().GetSafe(), MigrationRequired: data.GetUpgrade().GetMode() == api.DataUpgradeContractModeEnum_DATA_UPGRADE_CONTRACT_MODE_ENUM_PUBLISHER_MIGRATION})
}

// Historical default App starts omitted RuntimeContract from the descriptor and
// recorded empty DataCompatibility. Resolve only that exact immutable source;
// its current admission status does not change the schema it already wrote.
func (s *Service) replacementCurrentData(ctx context.Context, plan *replacementPlan, current *currentDelivery) error {
	descriptor := &api.DeploymentDescriptor{}
	if err := publicjson.Unmarshal(current.Descriptor, descriptor); err != nil {
		return status.Error(codes.DataLoss, "current application descriptor is invalid")
	}
	if plan.runtimeVersionID != "" && !contracts.IsDefaultOPLAppSelection(descriptor.GetApplicationSelection()) {
		return status.Errorf(codes.FailedPrecondition, "%s: a Default App upgrade requires an existing Default App", ReasonReplacementUnavailable)
	}
	if descriptor.GetProvenance() != api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_RUNTIME_RELEASE {
		return nil
	}
	contract := descriptor.GetRuntimeContract()
	if contract == nil {
		if s.RuntimeReleases == nil {
			return status.Error(codes.Unavailable, "Runtime Control is not configured")
		}
		sourceID, cursor := descriptor.GetApplicationSelection().GetRuntimeVersionId(), ""
		if sourceID == "" {
			return status.Error(codes.DataLoss, "current App has no frozen Runtime Release identity")
		}
		for contract == nil {
			page, err := s.RuntimeReleases.ListRuntimeVersions(ctx, &api.ListRuntimeVersionsRpcRequest{Context: nextOwnerCall(plan.call), QueryCursor: &cursor})
			if err != nil {
				return err
			}
			for _, release := range page.GetItems() {
				if release.GetId() == sourceID {
					contract = release.GetPublisherContract()
					break
				}
			}
			if contract != nil {
				break
			}
			if page.GetNextCursor() == "" {
				return status.Errorf(codes.FailedPrecondition, "%s: original Runtime Release data contract is unavailable", ReasonDataCompatibilityUnproven)
			}
			cursor = page.GetNextCursor()
		}
	}
	if !proto.Equal(contract.GetImage(), descriptor.GetArtifact()) || !proto.Equal(contract.GetApplicationRevisionTemplate(), descriptor.GetApplicationRevision()) {
		return status.Error(codes.DataLoss, "original Runtime Release differs from the frozen application")
	}
	compatibility, err := runtimeReplacementCompatibility(contract)
	if err != nil {
		return err
	}
	descriptor.RuntimeContract = contract
	current.Descriptor, err = publicjson.Marshal(descriptor)
	if err != nil {
		return err
	}
	current.Compatibility = compatibility
	if plan.runtimeVersionID != "" && descriptor.GetApplicationRevision().GetApplicationId() != plan.descriptor.GetApplicationRevision().GetApplicationId() {
		return status.Errorf(codes.FailedPrecondition, "%s: a Default App update must retain its application data identity", ReasonDataCompatibilityUnproven)
	}
	if plan.runtimeVersionID != "" {
		// The credential generation is inherited by the existing runtime lineage.
		// Its names, paths and login identity must remain the same ABI, otherwise
		// the retained password/session files would no longer name the same facts.
		old := descriptor.GetApplicationRevision().GetCredentials()
		next := plan.descriptor.GetApplicationRevision().GetCredentials()
		if len(old) != len(next) {
			return status.Error(codes.FailedPrecondition, "Default App upgrade must retain the original credential declarations")
		}
		byName := map[string]*api.WorkspaceApplicationCredential{}
		for _, credential := range old {
			byName[credential.GetName()] = credential
		}
		for _, credential := range next {
			if !proto.Equal(byName[credential.GetName()], credential) {
				return status.Error(codes.FailedPrecondition, "Default App upgrade must retain credential names, kinds, targets and login identity")
			}
		}
	}
	return nil
}

// prepareReplacementState uses only the exact predecessor's frozen execution and
// confirmed reload record. It neither creates a Key nor binds a new Secret.
func (s *Service) prepareReplacementState(ctx context.Context, q rowQueryer, plan *replacementPlan, current *currentDelivery) error {
	var pendingReload bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM serve.agent_runtime_actions WHERE runtime_instance_id=$1 AND expected_deployment_id=$2 AND action='reload' AND observation_result<>'confirmed')`, current.RuntimeID, current.DeploymentID).Scan(&pendingReload); err != nil {
		return dbError(err)
	}
	if pendingReload {
		return status.Error(codes.FailedPrecondition, "resume the original model reload before replacing its writer")
	}
	if err := s.replacementCurrentData(ctx, plan, current); err != nil {
		return err
	}
	var raw []byte
	if err := q.QueryRowContext(ctx, `SELECT input_snapshot FROM serve.agent_runtime_actions WHERE command_id=$1 AND runtime_instance_id=$2 AND expected_deployment_id=$3`, stableID("start_", current.DeploymentID), current.RuntimeID, current.DeploymentID).Scan(&raw); err != nil {
		return status.Error(codes.FailedPrecondition, "original application execution identity is unavailable")
	}
	command, _, err := decodeRuntimeStartSnapshot(raw)
	if err != nil {
		return err
	}
	if command.GetWorkspaceId() != plan.workspaceID || command.GetDeploymentId() != current.DeploymentID || command.GetRuntimeInstanceId() != current.RuntimeID || command.GetResourceSetId() != current.ResourceSetID || command.GetDataAttachmentId() != current.DataAttachment {
		return status.Error(codes.DataLoss, "original application resource identity mismatch")
	}
	inherited := proto.Clone(command).(*api.RuntimeDeployCommand)
	var applied int64
	if err = q.QueryRowContext(ctx, `SELECT applied_model_configuration_version FROM serve.agent_runtime_instances WHERE id=$1 AND deployment_id=$2`, current.RuntimeID, current.DeploymentID).Scan(&applied); err != nil {
		return dbError(err)
	}
	var reload map[string]string
	err = q.QueryRowContext(ctx, `SELECT input_snapshot FROM serve.agent_runtime_actions WHERE runtime_instance_id=$1 AND expected_deployment_id=$2 AND action='reload' AND observation_result='confirmed' ORDER BY created_at DESC,id DESC LIMIT 1`, current.RuntimeID, current.DeploymentID).Scan(&raw)
	if err == nil {
		if json.Unmarshal(raw, &reload) != nil {
			return status.Error(codes.DataLoss, "confirmed predecessor reload is invalid")
		}
		version, parseErr := strconv.ParseInt(reload["targetVersion"], 10, 64)
		if parseErr != nil || version <= 0 || version != applied {
			return status.Error(codes.DataLoss, "confirmed predecessor reload does not match its applied model version")
		}
		// Existing reload records retain a digest, not the model selections. Never
		// silently revert a customer's later models to the initial start values.
		if reload["selections"] != reloadSelectionDigest(inherited.GetModelSelections()) {
			return status.Error(codes.FailedPrecondition, "current model selections cannot be reconstructed from the stored reload digest")
		}
		inherited.ModelConfigurationVersion = version
	} else if !errors.Is(err, sql.ErrNoRows) {
		return dbError(err)
	} else if applied != inherited.GetModelConfigurationVersion() {
		return status.Error(codes.DataLoss, "predecessor applied model version has no confirmed execution snapshot")
	}
	credential, declared, err := declaredGatewayCredential(&api.RuntimeDeployCommand{DeploymentDescriptor: plan.descriptor})
	if err != nil {
		return err
	}
	if !declared {
		inherited.ManagedKeyBinding = nil
	} else {
		binding := inherited.GetManagedKeyBinding()
		if binding == nil || binding.GetKeyBindingId() == "" || binding.GetSecretBindingId() == "" || binding.GetSecretVersion() == "" || binding.GetFingerprint() == "" || binding.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef(plan.workspaceID) || binding.GetTargetSlot() != credential.Name {
			return status.Errorf(codes.FailedPrecondition, "%s: original confirmed Gateway binding does not match the target declaration", ReasonManagedKeyUnavailable)
		}
		if reload != nil {
			if strings.TrimSpace(reload["keyBinding"]) == "" || strings.TrimSpace(reload["fingerprint"]) == "" || strings.TrimSpace(reload["secretBindingId"]) == "" || strings.TrimSpace(reload["secretVersion"]) == "" {
				return status.Error(codes.DataLoss, "confirmed predecessor reload has no complete Gateway binding")
			}
			binding.KeyBindingId, binding.Fingerprint = reload["keyBinding"], reload["fingerprint"]
			binding.SecretBindingId, binding.SecretVersion = reload["secretBindingId"], reload["secretVersion"]
		}
	}
	plan.inherited = inherited
	return nil
}

func replacementPredecessor(ctx context.Context, q rowQueryer, deploymentID, workspaceID string) (*currentDelivery, error) {
	var previous string
	if err := q.QueryRowContext(ctx, `SELECT previous_deployment_id FROM serve.agent_deployments WHERE id=$1 AND workspace_id=$2`, deploymentID, workspaceID).Scan(&previous); err != nil {
		return nil, dbError(err)
	}
	current, err := existingReplacementTx(ctx, q, previous, workspaceID)
	if err == nil && current == nil {
		err = status.Error(codes.DataLoss, "replacement predecessor is unavailable")
	}
	return current, err
}

func (s *Service) validateReplacementReplay(ctx context.Context, q rowQueryer, plan replacementPlan, replay *currentDelivery) error {
	var previous, capability, runtime string
	if err := q.QueryRowContext(ctx, `SELECT previous_deployment_id,capability_version_id,COALESCE(runtime_version_id,'') FROM serve.agent_deployments WHERE id=$1 AND workspace_id=$2`, replay.DeploymentID, plan.workspaceID).Scan(&previous, &capability, &runtime); err != nil {
		return dbError(err)
	}
	descriptor := &api.DeploymentDescriptor{}
	if publicjson.Unmarshal(replay.Descriptor, descriptor) != nil {
		return status.Error(codes.DataLoss, "replacement descriptor is invalid")
	}
	if previous != plan.expectedCurrent || capability != plan.capabilityVersionID || runtime != plan.runtimeVersionID || !proto.Equal(descriptor, plan.descriptor) {
		return status.Error(codes.AlreadyExists, "replacement input differs from its original command")
	}
	return nil
}

// The predecessor decision is part of this accepted operation, before provider
// calls. Recovery never reselects credentials from a later predecessor reload.
type replacementInput struct {
	reservationInput
	ReplacementState  json.RawMessage `json:"replacementState"`
	ExclusiveWriter   bool            `json:"exclusiveWriter"`
	AcceptanceVersion int             `json:"acceptanceVersion,omitempty"`
	ExpectedCurrent   string          `json:"expectedCurrent,omitempty"`
	Compatibility     json.RawMessage `json:"compatibility,omitempty"`
}

func restoreReplacementState(ctx context.Context, tx *sql.Tx, operationID string, plan *replacementPlan, current *currentDelivery) error {
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT accepted_input FROM serve.operations WHERE id=$1 FOR UPDATE`, operationID).Scan(&raw); err != nil {
		return dbError(err)
	}
	var input replacementInput
	if json.Unmarshal(raw, &input) != nil {
		return status.Error(codes.DataLoss, "replacement accepted input is invalid")
	}
	if len(input.ReplacementState) == 0 {
		return migrateLegacyReplacementState(ctx, tx, operationID, raw, input, plan, current)
	}
	command := &api.RuntimeDeployCommand{}
	if protojson.Unmarshal(input.ReplacementState, command) != nil || command.GetWorkspaceId() != plan.workspaceID || command.GetDeploymentId() != current.DeploymentID || command.GetRuntimeInstanceId() != current.RuntimeID || command.GetResourceSetId() != current.ResourceSetID || command.GetDataAttachmentId() != current.DataAttachment {
		return status.Error(codes.DataLoss, "replacement predecessor state does not match its resource identity")
	}
	plan.inherited, plan.exclusive = command, input.ExclusiveWriter
	return nil
}

// The old Agent admission committed the reservation before binding its claim and
// recording start. Its reservation is sufficient to resume that delivery, but
// must not be interpreted as a native admission with a missing predecessor.
// Both fields absent is the retained format, not proof of a record's age: loss
// of both fields from a new Agent is indistinguishable. Require independently
// matching owner evidence and an unmodified predecessor, so reconstruction is
// deterministic in either case; never guess a later model or credential state.
func migrateLegacyReplacementState(ctx context.Context, tx *sql.Tx, operationID string, raw []byte, input replacementInput, plan *replacementPlan, current *currentDelivery) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || plan.capabilityVersionID == "" || plan.runtimeVersionID != "" {
		return status.Error(codes.DataLoss, "replacement has no frozen predecessor state")
	}
	for key := range fields {
		switch key {
		case "request", "actor", "scope", "authorizationContextId", "digest":
		default:
			return status.Error(codes.DataLoss, "replacement is not a retained Agent admission")
		}
	}
	request, scope := &api.RuntimeReservationCommand{}, &api.AuthorizationScope{}
	if protojson.Unmarshal(input.Request, request) != nil || protojson.Unmarshal(input.Scope, scope) != nil || input.Actor != plan.call.GetActorId() || !proto.Equal(scope, plan.call.GetScope()) || input.Digest != "sha256:"+ownerstore.HashRequestBody(reservationBytes(request)) {
		return status.Error(codes.DataLoss, "legacy replacement admission evidence is invalid")
	}
	var deploymentID string
	if err := tx.QueryRowContext(ctx, `SELECT resource_id FROM serve.operations WHERE id=$1 AND kind=$2 AND actor_id=$3`, operationID, plan.kind, input.Actor).Scan(&deploymentID); err != nil {
		return dbError(err)
	}
	selection := request.GetApplicationSelection()
	if request.GetWorkspaceId() != plan.workspaceID || request.GetDeploymentId() != deploymentID || request.GetCapabilityVersionId() != plan.capabilityVersionID || request.GetResourceSetId() != current.ResourceSetID || request.GetDataAttachmentId() != current.DataAttachment || request.GetDeploymentDescriptorDigest() != plan.descriptorDigest || request.GetDeploymentDescriptorObjectRef() != plan.descriptorRef || !proto.Equal(request.GetDeploymentDescriptor(), plan.descriptor) || !proto.Equal(request.GetArtifact(), plan.descriptor.GetArtifact()) || selection.GetKind() != api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_AGENT || selection.GetCapabilityVersionId() != plan.capabilityVersionID || selection.GetRuntimeVersionId() != "" {
		return status.Error(codes.DataLoss, "legacy replacement reservation differs from its admitted delivery")
	}
	var start []byte
	var applied int64
	var reloaded bool
	if err := tx.QueryRowContext(ctx, `SELECT a.input_snapshot,r.applied_model_configuration_version,
 EXISTS(SELECT 1 FROM serve.agent_runtime_actions x WHERE x.runtime_instance_id=r.id AND x.action='reload')
 FROM serve.agent_runtime_actions a JOIN serve.agent_runtime_instances r ON r.id=a.runtime_instance_id
 WHERE a.command_id=$1 AND a.action='start' AND a.runtime_instance_id=$2 AND a.expected_deployment_id=$3 AND r.deployment_id=$3 AND r.workspace_id=$4 AND r.execution_epoch=$5`, stableID("start_", current.DeploymentID), current.RuntimeID, current.DeploymentID, plan.workspaceID, current.ExecutionEpoch).Scan(&start, &applied, &reloaded); err != nil {
		return dbError(err)
	}
	command, _, err := decodeRuntimeStartSnapshot(start)
	if err != nil {
		return err
	}
	predecessor := &api.DeploymentDescriptor{}
	if publicjson.Unmarshal(current.Descriptor, predecessor) != nil || !proto.Equal(predecessor, command.GetDeploymentDescriptor()) || command.GetWorkspaceId() != plan.workspaceID || command.GetDeploymentId() != current.DeploymentID || command.GetRuntimeInstanceId() != current.RuntimeID || command.GetResourceSetId() != current.ResourceSetID || command.GetDataAttachmentId() != current.DataAttachment || command.GetExecutionEpoch() != current.ExecutionEpoch {
		return status.Error(codes.DataLoss, "legacy predecessor start differs from its admitted delivery")
	}
	if reloaded || applied != command.GetModelConfigurationVersion() {
		return status.Error(codes.DataLoss, "legacy replacement predecessor has no unambiguous frozen model state")
	}
	credential, declared, err := declaredGatewayCredential(&api.RuntimeDeployCommand{DeploymentDescriptor: plan.descriptor})
	if err != nil {
		return err
	}
	if !declared {
		command.ManagedKeyBinding = nil
	} else {
		binding := command.GetManagedKeyBinding()
		if binding.GetKeyBindingId() == "" || binding.GetSecretBindingId() == "" || binding.GetSecretVersion() == "" || binding.GetFingerprint() == "" || binding.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef(plan.workspaceID) || binding.GetTargetSlot() != credential.Name {
			return status.Error(codes.DataLoss, "legacy predecessor has no matching committed Gateway binding")
		}
	}
	command.Context = nil
	exclusive, err := exclusiveDataWriter(current.Descriptor, plan.descriptor)
	if err != nil {
		return err
	}
	var action, runtimeID, previous, observation string
	var stop []byte
	err = tx.QueryRowContext(ctx, `SELECT action,runtime_instance_id,expected_deployment_id,observation_result,input_snapshot FROM serve.agent_runtime_actions WHERE command_id=$1`, stableID("stop_", deploymentID)).Scan(&action, &runtimeID, &previous, &observation, &stop)
	if err == nil {
		var intent map[string]string
		if json.Unmarshal(stop, &intent) != nil || action != "stop" || runtimeID != current.RuntimeID || previous != current.DeploymentID || observation == "rejected" || len(intent) != 4 || intent["action"] != "stop" || intent["runtimeInstanceId"] != runtimeID || intent["deploymentId"] != previous || intent["replacementDeploymentId"] != deploymentID {
			return status.Error(codes.DataLoss, "legacy replacement stop differs from its predecessor")
		}
		exclusive = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		return dbError(err)
	}
	// Preserve the original reservation bytes, digest and reference evidence. The
	// migrated predecessor is committed before any bind, stop or start is retried.
	fields["replacementState"] = deployBytes(command)
	fields["exclusiveWriter"], err = json.Marshal(exclusive)
	if err != nil {
		return err
	}
	migrated, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE serve.operations SET accepted_input=$2 WHERE id=$1`, operationID, migrated); err != nil {
		return dbError(err)
	}
	plan.inherited, plan.exclusive = command, exclusive
	return nil
}

func (s *Service) restoreReplacementSource(ctx context.Context, plan *replacementPlan) (bool, error) {
	var accepted []byte
	err := s.DB.QueryRowContext(ctx, `SELECT accepted_input FROM serve.operations WHERE id=$1 AND kind='update_workspace'`, replacementOperationID(*plan)).Scan(&accepted)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, dbError(err)
	}
	if err == nil {
		var input replacementInput
		if json.Unmarshal(accepted, &input) != nil {
			return true, status.Error(codes.DataLoss, "replacement accepted input is invalid")
		}
		if input.AcceptanceVersion != 0 {
			return true, restoreAcceptedReplacement(input, plan)
		}
		if input.ExpectedCurrent != "" || len(input.Compatibility) != 0 {
			return true, status.Error(codes.DataLoss, "replacement acceptance version is missing")
		}
	}
	var capability, runtime, previous string
	var raw []byte
	err = s.DB.QueryRowContext(ctx, `SELECT d.capability_version_id,COALESCE(d.runtime_version_id,''),d.previous_deployment_id,i.deployment_descriptor,i.deployment_descriptor_digest,i.deployment_descriptor_object_ref,d.data_compatibility
 FROM serve.operations o JOIN serve.agent_deployments d ON d.id=o.resource_id JOIN serve.agent_runtime_instances i ON i.deployment_id=d.id
 WHERE o.id=$1 AND d.workspace_id=$2`, replacementOperationID(*plan), plan.workspaceID).Scan(&capability, &runtime, &previous, &raw, &plan.descriptorDigest, &plan.descriptorRef, &plan.compatibility)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, dbError(err)
	}
	if capability != plan.capabilityVersionID || runtime != plan.runtimeVersionID || previous != plan.expectedCurrent {
		return true, status.Error(codes.AlreadyExists, "replacement input differs from its original command")
	}
	plan.descriptor = &api.DeploymentDescriptor{}
	if publicjson.Unmarshal(raw, plan.descriptor) != nil {
		return true, status.Error(codes.DataLoss, "frozen replacement descriptor is invalid")
	}
	digest, err := descriptorDigest(plan.descriptor)
	if err != nil || digest != plan.descriptorDigest {
		return true, status.Error(codes.DataLoss, "frozen replacement descriptor digest mismatch")
	}
	return true, nil
}
